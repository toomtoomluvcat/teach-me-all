package generation

import (
	"fmt"
	"strings"
)

// Schemas are plain Go maps so they can be handed straight to Ollama's
// structured-output `format` field without a round trip through a struct tag
// reflector. Keeping them next to the prompt that references them is
// deliberate: when a prompt changes, its schema usually has to change too.

// obj is a small helper to keep the schema literals readable.
func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func str(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func enum(desc string, values ...string) map[string]any {
	vals := make([]any, len(values))
	for i, v := range values {
		vals[i] = v
	}
	return map[string]any{"type": "string", "description": desc, "enum": vals}
}

// --- pass 1: map ------------------------------------------------------------

func topicListSchema() map[string]any {
	return map[string]any{
		"type":        "array",
		"minItems":    1,
		"maxItems":    3,
		"description": "the distinct topics this passage covers",
		"items":       str("a short topic title, in the same language as the passage"),
	}
}

func passageKindSchema() map[string]any {
	return enum("what this passage is: content teaches the subject, apparatus is teacher-facing machinery about the subject, non_content is page furniture",
		string(TopicContent), string(TopicApparatus), string(TopicNonContent))
}

// TopicSchema is what the model returns for a single chunk during the map step.
func TopicSchema() map[string]any {
	return obj(map[string]any{
		"kind":   passageKindSchema(),
		"topics": topicListSchema(),
	}, "kind", "topics")
}

const topicSystem = `You are indexing an educational source so it can be split into lessons.

The source may be a student textbook, reference work, teacher guide, lab manual,
or mixed document in any subject. Do not assume what kind of source or subject
this is from prior tasks. Read the passage, say what it is, and name the topics
it covers. Rules:
- Name only topics the passage actually covers. Do not infer topics from a
  heading that has no material under it.
- Write the titles in the same language as the passage.
- A title is a noun phrase, not a sentence, and not a question.

Then set kind — one judgement about the whole passage, not about each topic:

- "content" — subject matter a learner is meant to understand. Facts,
  mechanisms, definitions, worked examples, experimental results, and the
  material of a laboratory activity are all content, including background depth
  written for the teacher.
- "apparatus" — teacher-facing machinery ABOUT the subject rather than the
  subject itself: answer keys and worked solutions to exercises, assessment or
  measurement guidance, scoring rubrics and criteria, banks of test items,
  learning objectives and outcomes, lesson plans, suggested teaching sequences,
  preparation notes, and how many hours to spend.
- "non_content" — page furniture: covers, front matter, tables of contents,
  running headers, indexes.

Judge what the passage IS, not what it is about. A page of answers to exercises
is apparatus even though every answer is about the subject. A laboratory or
worked example that explains what happens and why is content even if a teacher
would normally facilitate it. If the passage explains the subject anywhere
inside it — states a mechanism, names an entity and its function, gives data or
a reason, or demonstrates a method — it is content. Mark apparatus only when
the passage would still say nothing about the subject after removing the
instructions: a bare answer key, scoring table, checklist of objectives,
schedule, or teacher-only procedure.

When you cannot decide, choose content.`

// TopicPrompt builds the map-step user message.
func TopicPrompt(c Chunk) string {
	return fmt.Sprintf("Passage (page %d):\n\n%s", c.Page, c.Text)
}

func TopicSystem() string { return topicSystem }

// TopicBatchSchema is the batched-provider map response. Keeping chunk_id in the
// response lets the provider return results in any order while the pipeline
// still restores document order deterministically.
func TopicBatchSchema() map[string]any {
	return obj(map[string]any{
		"chunks": map[string]any{
			"type":     "array",
			"minItems": 1,
			"items": obj(map[string]any{
				"chunk_id": str("the exact chunk ID supplied in the input"),
				"kind":     passageKindSchema(),
				"topics":   topicListSchema(),
			}, "chunk_id", "kind", "topics"),
		},
	}, "chunks")
}

func TopicBatchSystem() string {
	return topicSystem + `

You will receive multiple labelled passages in one request. Return exactly one
result for every chunk_id. Copy each chunk_id character for character. Do not
merge chunks, skip chunks, or put topics from one chunk under another chunk. A
passage that is entirely apparatus or page furniture still gets its chunk_id
back, with its own kind and topics — never an empty list. Return exactly this
JSON shape, with no other top-level keys:
{"chunks":[{"chunk_id":"p30-c0","kind":"content","topics":["topic title"]}]}.
This is only a shape example: use every actual chunk_id supplied below. Count
the objects before answering; the number must equal the number of passages.`
}

// TopicBatchPrompt renders all map chunks into one provider request. This is
// intentionally separate from TopicPrompt: Ollama keeps its measured one-call
// per chunk path, while hosted providers use their larger context windows to
// save calls.
func TopicBatchPrompt(chunks []Chunk) string {
	var b strings.Builder
	for _, c := range chunks {
		fmt.Fprintf(&b, "Chunk %s (page %d):\n\n%s\n\n", c.ID, c.Page, c.Text)
	}
	return b.String()
}

// --- pass 1: reduce ---------------------------------------------------------

// OutlineSchema is what the model returns when folding topics into lessons.
func OutlineSchema() map[string]any {
	return obj(map[string]any{
		"course_title": str("a title for the whole course, in the source language"),
		"lessons": map[string]any{
			"type":     "array",
			"minItems": 1,
			"items": obj(map[string]any{
				"title":   str("lesson title, in the source language"),
				"summary": str("one sentence on what this lesson teaches"),
				"question_budget": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     20,
					"description": "how many exam questions this lesson genuinely supports, based on how much distinct material it contains. Be honest: a thin lesson gets a small number.",
				},
				"concept_ids": map[string]any{
					"type":        "array",
					"minItems":    1,
					"description": "the stable concept IDs from the evidence graph that belong to this lesson",
					"items":       str("a concept ID copied exactly, such as C001"),
				},
			}, "title", "summary", "question_budget", "concept_ids"),
		},
	}, "course_title", "lessons")
}

const outlineSystem = `You are compiling a source-grounded concept graph from a textbook into a course outline.

Rules:
- Group related concept nodes into lessons that could each be taught in one sitting.
- Keep the original order of the material. Do not reorder topics to make
  tidier groups.
- Every concept ID in the input must appear in exactly one lesson.
- Copy concept IDs verbatim. Titles are descriptions; IDs are the join keys.
- Use co_occurs and follows edges as structural evidence when choosing groups.
- Write lesson titles and summaries in the same language as the concept titles.
- Prefer fewer, substantial lessons over many thin ones.
- Set question_budget to the number of exam questions the lesson honestly
  supports. Do not pad it to look generous — a lesson covering one definition
  supports one or two questions, not ten.`

func OutlineSystem() string { return outlineSystem }

func OutlinePrompt(graph EvidenceGraph) string {
	var b strings.Builder
	b.WriteString("Concept nodes, in first-evidence order:\n")
	for _, concept := range graph.Concepts {
		fmt.Fprintf(&b, "- %s | %s | pages %v\n", concept.ID, concept.Title, concept.Pages)
	}
	b.WriteString("\nEvidence edges:\n")
	for _, edge := range graph.Edges {
		fmt.Fprintf(&b, "- %s -> %s | %s\n", edge.From, edge.To, edge.Kind)
	}
	return b.String()
}

// EvidenceCompileSystem is deliberately separate from question writing. Its
// job is to turn prose into source-bound claims and possible reasoning modes;
// it must not solve an assessment or invent facts from general knowledge.
func EvidenceCompileSystem() string {
	return `You are compiling a domain-agnostic educational source into atomic evidence.
The source may teach any subject. Infer the domain from the supplied passages.
Split each passage into the smallest meaningful teachable claims: definitions,
mechanisms, relationships, equations, conditions, sequences, observations, or
worked-example rules. Preserve the exact source chunk ID for every atom.

Rules:
- Every claim must be directly supported by one supplied chunk.
- evidence_quote must be an exact contiguous substring from that same chunk;
  never paraphrase it.
- Do not merge unrelated claims or add facts from memory.
- relation is a short label such as definition, equation, causal, sequence,
  condition, comparison, direction, observation, or example.
- variables and conditions name only quantities or constraints present in the
  passage.
- question_forms lists honest forms the claim supports: recall, understanding,
  application, calculation. Use calculation only when the passage gives
  enough numbers or an explicit equation.
- Return no atom for page furniture, learning objectives, answer keys, or
  assessment instructions.`
}

func EvidenceCompileSchema() map[string]any {
	atom := obj(map[string]any{
		"source_chunk_id": str("exact supplied chunk ID containing this claim"),
		"concept_ids": map[string]any{
			"type":  "array",
			"items": str("one supplied concept ID"),
		},
		"claim":          str("one short source-supported teachable claim"),
		"evidence_quote": str("exact contiguous substring copied character-for-character from the same source chunk that supports the claim"),
		"relation":       enum("relationship type", "definition", "equation", "causal", "sequence", "condition", "comparison", "direction", "observation", "example"),
		"conditions":     map[string]any{"type": "array", "items": str("source-stated constraint")},
		"variables":      map[string]any{"type": "array", "items": str("source-stated variable or quantity")},
		"question_forms": map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": enum("supported reasoning form", "recall", "understanding", "application", "calculation")},
	}, "source_chunk_id", "concept_ids", "claim", "evidence_quote", "relation", "conditions", "variables", "question_forms")
	return obj(map[string]any{
		"atoms": map[string]any{
			"type":     "array",
			"minItems": 1,
			"maxItems": 80,
			"items":    atom,
		},
	}, "atoms")
}

func EvidenceCompilePrompt(graph EvidenceGraph, chunks []Chunk) string {
	var b strings.Builder
	b.WriteString("Concept IDs available in the graph:\n")
	for _, concept := range graph.Concepts {
		fmt.Fprintf(&b, "- %s: %s\n", concept.ID, concept.Title)
	}
	b.WriteString("\nPassages to compile:\n")
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "[%s | page %d]\n%s\n\n", chunk.ID, chunk.Page, chunk.Text)
	}
	b.WriteString("Return one atom per distinct teachable claim. Do not draft questions.")
	return b.String()
}

// QuestionSetSystem adds only what the per-run slot execution protocol in
// QuestionSetPrompt does not already say.
//
// It used to repeat that protocol almost line for line — process slots in
// order, copy the ID tuple, copy skill and the calculation flag, fill
// changed_condition and reasoning_steps for hard application, do not repeat
// questions on a retry — roughly 300 tokens of the same instructions arriving
// twice in the same request. Two statements of a rule are not twice the rule;
// they are one rule and a maintenance hazard, since editing the protocol and
// forgetting this block leaves the model holding two versions of the contract.
// What stays here is what the protocol has no place for: the framing that this
// is one set rather than a pile of questions, and the three ways a slot can be
// answered wrongly while still looking answered.
func QuestionSetSystem() string {
	return questionSystem + `

You are writing a complete assessment set, not independent questions. The
coverage contract is authoritative: use each slot at most once and never reuse
the same claim, relationship, or scenario with different numbers.

A quote being present is not enough — it must support the answer target. If a
slot is not supported by its atom and chunk, omit that slot rather than
substituting a repeated one. The operation label is a contract value to copy
exactly, not a free-text explanation. Do not add arithmetic to a slot whose
requires_calculation flag is false.`
}

// QuestionSetSchemaForContract adds the finite ID vocabularies for a specific
// contract. Ollama can enforce these through its grammar; providers that only
// support JSON mode still get the same contract in the prompt and deterministic
// gates remain authoritative.
//
// Read that second sentence literally before treating this schema as a lever:
// OpenAIClient.ChatJSON takes the schema and never sends it, setting only
// response_format={"type":"json_object"}. On DeepSeek and every other
// OpenAI-compatible provider, nothing here constrains generation at all.
// Marking a field required in this map was measured and changed nothing — what
// the writer actually follows is the JSON template at the end of
// questionSystem, so a field that must be emitted has to appear there.
func QuestionSetSchemaForContract(forceCalc bool, contract CoverageContract) map[string]any {
	return questionSetSchema(forceCalc, &contract)
}

func questionSetSchema(forceCalc bool, contract *CoverageContract) map[string]any {
	schema := questionSchema(forceCalc)
	root := schema["properties"].(map[string]any)
	questions := root["questions"].(map[string]any)
	item := questions["items"].(map[string]any)
	properties := item["properties"].(map[string]any)
	properties["coverage_slot_id"] = contractIDSchema("exact coverage slot ID from the contract", contractIDs(contract, func(slot CoverageSlot) string { return slot.ID }))
	properties["evidence_atom_id"] = contractIDSchema("exact evidence atom ID assigned to the selected slot", contractIDs(contract, func(slot CoverageSlot) string { return slot.AtomID }))
	properties["evidence_chunk_id"] = contractIDSchema("exact source chunk ID containing the quote for the selected slot", contractIDs(contract, func(slot CoverageSlot) string {
		if len(slot.SourceChunkIDs) > 0 {
			return slot.SourceChunkIDs[0]
		}
		return ""
	}))
	properties["operation"] = contractIDSchema("copy the exact operation label from the assigned coverage slot", contractOperations(contract))
	properties["requires_calculation"] = map[string]any{"type": "boolean", "description": "copy the exact requires_calculation flag from the assigned coverage slot"}
	properties["supporting_atom_ids"] = map[string]any{
		"type": "array", "maxItems": 3,
		"items": contractIDSchema("exact supporting atom ID from the assigned hard slot", supportAtomIDs(contract)),
	}
	required := append([]string{}, item["required"].([]string)...)
	required = append(required, "coverage_slot_id", "evidence_atom_id", "evidence_chunk_id", "operation", "requires_calculation")
	if contractNeedsDemand(contract) {
		required = append(required, "reasoning_steps", "changed_condition", "distractor_reasons")
	}
	if contractNeedsHardSupport(contract) {
		required = append(required, "supporting_atom_ids")
	}
	item["required"] = required
	return schema
}

func contractIDSchema(description string, values []string) map[string]any {
	if len(values) == 0 {
		return str(description)
	}
	return enum(description, values...)
}

func contractIDs(contract *CoverageContract, pick func(CoverageSlot) string) []string {
	if contract == nil {
		return nil
	}
	seen := map[string]bool{}
	values := make([]string, 0, len(contract.Slots))
	for _, slot := range contract.Slots {
		value := strings.TrimSpace(pick(slot))
		if value != "" && !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}
	return values
}

func contractOperations(contract *CoverageContract) []string {
	return contractIDs(contract, func(slot CoverageSlot) string { return slot.Operation })
}

func supportAtomIDs(contract *CoverageContract) []string {
	if contract == nil {
		return nil
	}
	seen := map[string]bool{}
	ids := make([]string, 0)
	for _, slot := range contract.Slots {
		for _, id := range slot.SupportAtomIDs {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func contractNeedsDemand(contract *CoverageContract) bool {
	if contract == nil {
		return false
	}
	for _, slot := range contract.Slots {
		if strings.EqualFold(slot.Skill, "analysis") {
			return true
		}
		if strings.EqualFold(slot.Skill, "application") && (strings.EqualFold(slot.Difficulty, "medium") || strings.EqualFold(slot.Difficulty, "hard")) {
			return true
		}
	}
	return false
}

func contractNeedsHardSupport(contract *CoverageContract) bool {
	if contract == nil {
		return false
	}
	for _, slot := range contract.Slots {
		if strings.EqualFold(slot.Skill, "analysis") && len(slot.SupportAtomIDs) > 0 {
			return true
		}
		if strings.EqualFold(slot.Skill, "application") && strings.EqualFold(slot.Difficulty, "hard") && len(slot.SupportAtomIDs) > 0 {
			return true
		}
	}
	return false
}

func QuestionSetPrompt(lesson Lesson, graph *EvidenceGraph, chunks []Chunk, contract CoverageContract, feedback []RejectedDraft, forceCalc bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lesson: %s\n\n", lesson.Title)
	// Source context sits at the very top, above the directive, the coverage
	// contract, and the evidence packet. The contract slots change on every
	// benchmark case, so anything below them is re-paid per case; the source
	// context is byte-identical across candidates and across cases for the
	// same lesson, so it must stay inside the provider's prompt-cache prefix.
	// It is ~45-50% of the generate-set prompt (a 9-case run paid it 37 times).
	b.WriteString("Source context (the cited quote must come from the exact evidence_chunk_id):\n")
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "[%s | page %d]\n%s\n\n", chunk.ID, chunk.Page, chunk.Text)
	}
	if strings.TrimSpace(contract.GenerationDirective) != "" {
		fmt.Fprintf(&b, "Benchmark/Run directive (must be obeyed while staying source-grounded): %s\n\n", strings.TrimSpace(contract.GenerationDirective))
	}
	b.WriteString("Coverage contract (one slot per distinct question):\n")
	for _, slot := range contract.Slots {
		fmt.Fprintf(&b, "- %s atom=%s support_atoms=%s chunk=%s skill=%s requires_calculation=%t difficulty=%s min_claims=%d min_decoys=%d distractors=%s operation=%s target=%s evidence_quote=%q\n",
			slot.ID, slot.AtomID, strings.Join(slot.SupportAtomIDs, ","), strings.Join(slot.SourceChunkIDs, ","), slot.Skill, slot.RequiresCalculation, slot.Difficulty,
			slot.MinDepth, slot.MinDecoys, discriminationLabel(slot), slot.Operation, slot.Target, slot.EvidenceQuote)
	}
	if atoms := contractEvidenceAtoms(graph, contract); len(atoms) > 0 {
		b.WriteString("\nEvidence packet for the assigned slots:\n")
		for _, atom := range atoms {
			fmt.Fprintf(&b, "- %s chunk=%s concepts=%s relation=%s claim=%s evidence_quote=%q\n", atom.ID, atom.ChunkID, strings.Join(atom.ConceptIDs, ","), atom.Relation, atom.Claim, atom.Quote)
		}
	}
	if len(feedback) > 0 {
		b.WriteString(rejectionMemoryBlock(feedback))
	}
	// The candidate marker goes here, after every block that is identical
	// between candidates, rather than near the top where it used to sit.
	// Providers cache on an exact prompt prefix, and one differing line early on
	// makes the whole evidence packet and source context uncacheable — which is
	// most of the input tokens, paid again for every candidate.
	if contract.Variant > 0 {
		fmt.Fprintf(&b, "This is candidate set %d. Explore a different valid question angle where the evidence supports it; do not mention candidate selection in the questions.\n\n", contract.Variant)
	}
	b.WriteString("Slot execution protocol:\n")
	b.WriteString("1. Work through the slots in order; use one output object for one slot.\n")
	b.WriteString("2. Copy the exact slot/atom/chunk ID tuple from that row; never invent or mix IDs.\n")
	b.WriteString("3. Copy the row skill and requires_calculation flag exactly. Copy difficulty when the row names one; when it is blank, report the difficulty the finished question honestly has. If the row is unsupported, omit only that row.\n")
	b.WriteString("3a. Satisfy the row's three demand columns: min_claims is how many distinct source claims the answer must genuinely need (copy every support atom when it is 2 or more); min_decoys is how many stem givens the solution must not use (list them in decoy_values); distractors=close means every wrong option must be one a student could actually land on -- on a numeric question give each one a distractor_expression, on a question without arithmetic give each one a distractor_atom_id naming the real source claim it comes from, and failing both give each one its own distinct distractor_reason.\n")
	b.WriteString("4. For medium or hard application, fill changed_condition; for hard application, copy every support atom ID and provide at least two distinct reasoning_steps. For medium and hard questions, provide one short distractor_reason per wrong choice, each naming a distinct mistake -- never repeat the same distractor_reason text twice in one question.\n")
	if forceCalc {
		b.WriteString("5. For a slot whose skill is not fixed above, choose skill by what solving the calculation demands: understanding if the numbers plug straight into a formula the source already states solved for the unknown; application if you must isolate an unknown by rearranging or chaining the relationship first.\n")
		b.WriteString("6. Before returning, verify every source_quote is verbatim in its cited chunk, every ID is non-empty, and no two choices in a question repeat the same text.\n\n")
	} else {
		b.WriteString("5. Before returning, verify every source_quote is verbatim in its cited chunk, every ID is non-empty, and no two choices in a question repeat the same text.\n\n")
	}
	fmt.Fprintf(&b, "Write up to %d questions, one for each supported slot. Keep the set varied and return empty only for an unsupported slot. ", len(contract.Slots))
	if forceCalc {
		b.WriteString("Every returned question must set requires_calculation=true and include a valid calculation expression.")
	} else {
		b.WriteString("For slots marked requires_calculation=true, calculation.expression and calculation.expected are mandatory. Use the calculation object exactly when the flag is true.")
	}
	return b.String()
}

// discriminationLabel words the discrimination axis for the writer. "close" and
// "ordinary" say what the option set has to look like; the internal tier names
// would only invite the model to echo them back as a label.
func discriminationLabel(slot CoverageSlot) string {
	if strings.EqualFold(strings.TrimSpace(slot.Discrimination), DiscriminationHigh) {
		return "close"
	}
	return "ordinary"
}

// contractEvidenceAtoms keeps the writer's claim vocabulary local to the
// slots it must fill. The surrounding source chunks remain available for
// cross-context reasoning, but unrelated atom rows no longer compete for
// attention or input tokens.
func contractEvidenceAtoms(graph *EvidenceGraph, contract CoverageContract) []EvidenceAtom {
	if graph == nil || len(contract.Slots) == 0 {
		return nil
	}
	byID := make(map[string]EvidenceAtom, len(graph.Atoms))
	for _, atom := range graph.Atoms {
		byID[atom.ID] = atom
	}
	seen := map[string]bool{}
	atoms := make([]EvidenceAtom, 0, len(contract.Slots))
	for _, slot := range contract.Slots {
		ids := append([]string{slot.AtomID}, slot.SupportAtomIDs...)
		for _, id := range ids {
			atom, ok := byID[id]
			if !ok || seen[atom.ID] {
				continue
			}
			seen[atom.ID] = true
			atoms = append(atoms, atom)
		}
	}
	return atoms
}

// --- pass 2: question-set generation ----------------------------------------

// questionSchema is the shared per-question shape the set schema builds on.
// forceCalc requires the arithmetic object and the requires_calculation flag at
// the schema level rather than by asking nicely in the prompt.
func questionSchema(forceCalc bool) map[string]any {
	calc := obj(map[string]any{
		"expression": str("the arithmetic to solve this question, as a plain expression using numbers, + - * / ^, parentheses, decimal points, pi, and only these functions: sin, cos, tan, sqrt, abs, exp, ln. No variables or units. Trigonometric arguments are in radians. Example: (1200*0.07)/12 or 20*sin(30*pi/180)"),
		"expected":   map[string]any{"type": "number", "description": "the value that expression produces"},
		"unit":       str("the unit or notation used for the answer when the source provides one; empty for a dimensionless result or when no unit applies"),
	}, "expression", "expected")

	question := map[string]any{
		"kind":                 enum("question shape", string(KindMCQSingle)),
		"stem":                 str("the question itself, understandable on its own without seeing the passage"),
		"explanation":          str("why the correct choice is correct, in the source language"),
		"source_quote":         str("an exact contiguous substring copied CHARACTER FOR CHARACTER from the passage that makes the correct answer true; prefer a complete sentence, but a shorter exact span is allowed. Must appear exactly."),
		"difficulty":           enum("honest reasoning load: easy=one direct inference or operation, medium=one relationship with a meaningful distinction, hard=two linked inferences or competing constraints, or (for analysis) two distinct source ideas combined", "easy", "medium", "hard"),
		"skill":                enum("honest reasoning mode: recall=fact, understanding=interpretation, application=new situation using a source relationship, analysis=combine two distinct facts or relationships from different parts of the source that neither part answers alone, error-finding=locate the mistake in a worked attempt the stem displays", "recall", "understanding", "application", "analysis", SkillErrorFinding),
		"flawed_expression":    str("for an error-finding question only: the arithmetic the mistaken work shown in the stem actually performs. It must evaluate to the wrong result the stem prints and must differ from calculation.expression, which stays the correct arithmetic."),
		"requires_calculation": map[string]any{"type": "boolean", "description": "true when arithmetic is necessary to answer; false otherwise"},
		"reasoning_steps": map[string]any{
			"type": "array", "maxItems": 4,
			"items": str("one short, concrete reasoning step; required for hard and useful for medium"),
		},
		"changed_condition": str("the specific value, entity, constraint, exception, or context changed from the cited source; required for application medium/hard"),
		"distractor_reasons": map[string]any{
			"type": "array", "maxItems": 3,
			"items": str("why a partially informed student might choose this wrong option; one short reason per distractor"),
		},
		"supporting_atom_ids": map[string]any{
			"type": "array", "maxItems": 3,
			"items": str("exact support atom ID from the assigned hard slot"),
		},
		"decoy_values": map[string]any{
			"type": "array", "maxItems": 3,
			"items": str("a quantity or condition the stem supplies that the correct solution never uses; it must appear in the stem and must not appear in calculation.expression"),
		},
		"choices": map[string]any{
			"type":     "array",
			"minItems": 4,
			"maxItems": 4,
			// Both distractor fields are required with an explicit empty-string
			// escape rather than left optional. A first live run left them
			// unset on every choice of every question: an optional field is one
			// the writer can decline to think about, and the whole point of
			// them is to make it think about each wrong option before writing
			// it. Requiring them forces the decision; "" records that neither
			// route applies, which is a different answer from silence.
			"items": obj(map[string]any{
				"content":    str("the choice text"),
				"is_correct": map[string]any{"type": "boolean"},
				"distractor_expression": str("REQUIRED FIELD. For a wrong numeric choice: the plain arithmetic expression a student who made one specific mistake would evaluate, in the same alphabet as calculation.expression. It must produce exactly the number printed in this choice and must NOT equal the correct value. Use the empty string \"\" on the correct choice and when this option is not numeric."),
				"distractor_atom_id":    str("REQUIRED FIELD. For a wrong non-numeric choice: the exact evidence atom ID of the real source claim this option is drawn from. It must be one of the atoms supplied in the context and must not be the slot's own atom. Use the empty string \"\" on the correct choice and when this option is not taken from a source claim."),
			}, "content", "is_correct", "distractor_expression", "distractor_atom_id"),
		},
	}

	required := []string{"kind", "stem", "choices", "explanation", "source_quote", "difficulty", "skill", "requires_calculation"}
	question["calculation"] = calc
	if forceCalc {
		required = append(required, "calculation")
	}

	return obj(map[string]any{
		"questions": map[string]any{
			"type":     "array",
			"minItems": 1,
			"items":    obj(question, required...),
		},
	}, "questions")
}

const questionSystem = `You are a domain-agnostic assessment writer. The source may
teach any subject: science, mathematics, medicine, engineering, history,
language, economics, or something else. Infer the domain, entities, notation,
units, and relationships from the supplied passage. Never apply a
subject-specific template or import facts, vocabulary, or question patterns
from a different subject.

The passage is the only evidence. A good question is self-contained for the
student, but its answer must depend on a specific fact, relationship, sequence,
condition, number, named entity, or example actually present in this passage.
Do not reward yourself for merely asking a question that a knowledgeable model
could answer from general subject knowledge.

Choose honest labels:
- recall: retrieve one source fact, name, value, or definition.
- understanding: interpret, explain, classify, compare, or predict from a
  relationship stated in the source.
- application: apply a source relationship to a genuinely new situation or
  changed values. It must require the student to predict, choose, compare, or
  calculate an outcome in that situation. A stem that only asks for a name,
definition, named part, label, or unchanged property is recall or
  understanding, not application. A stem that only asks "what is", "which is",
  or repeats a source example is not application unless the student must use the
  relationship to decide something new.
- analysis: combine two distinct facts, relationships, or mechanisms drawn
  from two different, clearly separate parts of the source -- not the same
  relationship restated, not one formula applied twice to two numbers. The
  answer must depend on genuinely needing both; a student who only had one of
  the two supporting_atom_id facts could not reach it. This is a kind of
  reasoning, not a difficulty tier by itself: combining two directly-stated,
  closely-linked facts can be easy or medium; combining facts that conflict or
  need real resolution to reconcile is hard. Report the difficulty honestly
  either way -- do not default to hard just because two facts are involved.
  It is a stricter bar than hard application at the same difficulty: hard
  application can be satisfied by one relationship used twice, analysis
  cannot.
- error-finding: build it in this order, and do not write the stem first.
    1. Write calculation.expression: the arithmetic the problem actually needs.
    2. Break it once, on purpose, to get flawed_expression -- swap an operand,
       flip an operator, drop a factor, use the wrong constant. It must be a
       different string that evaluates to a different number.
    3. Only now write the stem, showing the BROKEN work and the number it
       produces, and ask what is wrong with it.
  The trap to avoid: writing a stem in which the displayed work is correct and
  then asking what is wrong with it. That is not a question -- there is nothing
  wrong. Before you return the item, evaluate the equation you printed in the
  stem: if it equals calculation.expected, you have written the correct work
  and the item is rejected. requires_calculation stays true.
  The correct choice names the mistake; it does not restate the right number,
  and it carries no distractor_expression. Neither do the wrong choices: on this
  question type the options are diagnoses, not numbers.
- requires_calculation: set true only when arithmetic is necessary to answer;
  keep skill as recall, understanding, application, or analysis according to
  the cognitive demand. Set false when the item can be answered without
  arithmetic.
- easy: one direct inference or operation; medium: one relationship plus a
  meaningful distinction; hard: at least two linked inferences, constraints,
  or transformations. For easy application, change a condition or value from
  the source and ask for an outcome, not a named fact. For hard application,
  use at least two given inputs or conditions and make the student carry out at
  least two linked transformations or decisions. If one sentence of the source
  or one unchanged relationship answers it, it is not hard. For analysis,
  easy/medium needs at least two distinct reasoning_steps showing both facts
  were used; hard needs at least three, same as the linked-inference bar
  above.

Compact assessment contract:
- For application medium or hard, changed_condition must name the exact
  material condition changed from the cited source (value, entity, constraint,
  exception, or context). Do not call wording changes a changed condition.
  Analysis also requires changed_condition, naming what makes the combination
  of the two ideas necessary rather than generic.
- For medium, hard, and analysis, include one concise distractor_reason for
  each wrong choice. Each reason must name a plausible but wrong assumption,
  not merely say "incorrect". No two distractor_reasons in the same question
  may be the same text or paraphrase the same mistake -- each wrong choice
  needs its own distinct misconception, even when two choices are wrong for a
  related reason (e.g. one direction-confusion and one axis-confusion are
  different mistakes, not the same one twice).
- Every wrong choice must be traceable, by whichever route the subject allows.
  On a numeric question give it a distractor_expression: the arithmetic a
  student who made one specific mistake would actually evaluate, producing the
  exact number printed in that choice and differing from the keyed value. On a
  question with no arithmetic give it a distractor_atom_id: the real source
  claim the option is drawn from, so the wrong answers are true statements that
  do not answer this question. A distractor_atom_id must be a DIFFERENT atom
  from the slot's own atom_id: that atom is where the correct answer comes
  from, so citing it on a wrong choice claims the wrong choice is the answer.
  Pick a neighbouring claim from the evidence packet instead. Never attach
  either field to the correct choice. If an option can be traced neither way it
  is a guess, not a distractor — replace it with one that can.
- decoy_values lists givens the stem supplies that the solution never uses.
  Copy each one as the exact words or number that appear in your own stem, the
  same way source_quote copies the passage -- "0.80 m" or "a long rainy season"
  if that is what the stem says, never a summary such as "air resistance" for a
  stem that never uses those words. A value the solution consumes is a given,
  not a decoy. Add them only when the slot asks for them, keep them believable
  for the scenario, and make at least one wrong choice the result of using one.
- For hard application, include at least two distinct reasoning_steps in order
  and copy every supporting_atom_id assigned to the slot. The steps must be
  necessary to reach the keyed answer; adding connective words to a one-step
  explanation does not count.
- For analysis, include at least two distinct reasoning_steps (three if you
  report hard) and copy every supporting_atom_id assigned to the slot. The
  assigned supporting atoms come from a different part of the source than the
  primary evidence on purpose -- use both; do not write a question that
  quietly only needs one of them.

Hard requirements for every question:
- The stem stands alone. Never point at invisible material with phrases such as
  "according to the passage", "the text above", "this value", or "the diagram".
- Exactly one choice is correct. The other three must be plausible and wrong for
  a reason a student could articulate. Keep choices parallel in type and length:
  the correct choice must not be visibly longer than the wrong ones. Put the
  reasoning that justifies the answer in the explanation field, never in the
  correct choice text, and give each wrong choice a comparable clause so the
  keyed option does not stand out by length alone. A student who reads only the
  option lengths must not be able to pick the answer.
- Do not use all/none of the above, both A and B, synonyms of the answer, or
  choices that merely restate the stem with one word or number changed.
- Ask about subject matter only. Exclude learning objectives, assessment rules,
  teacher instructions, classroom activities, videos, tests, and chapter
  numbering. If the passage has no teachable subject matter, return no questions.
- When rejected draft memory is supplied, avoid every listed failure pattern and
  ask a materially different question; do not lightly repair a rejected draft.
- Within one response, vary the source relationship and answer target. Do not
  ask the same scenario or principle twice with different numbers or wording.
- source_quote is an exact contiguous substring copied character for character
  from this passage. Do not paraphrase, translate, or repair punctuation. Use
  core explanatory text, not answer keys or pre-learning checks.
- Write in the same language as the passage.

For application questions, include enough scenario facts for the student to use
the source relationship, and make the explanation show the reasoning in order.

When requires_calculation=true:
	- Put the arithmetic in calculation.expression using numbers, + - * / ^,
	  parentheses, decimal points, pi, and only these functions: sin, cos, tan,
	  sqrt, abs, exp, ln. Trigonometric arguments are in radians. No variables,
	  units, or other functions. The expression is checked independently.
	- calculation.expected is a bare JSON number, not a string and not a unit.
	- Copy the calculator value exactly into calculation.expected; do not round
	  it. If the student-facing choice is rounded, label it as approximate and
	  keep the exact expected value in the calculation object.
	- The correct choice contains a decimal or integer numeric approximation of
	  the computed value, plus the unit when one exists. Do not use radicals,
	  variables, or symbolic expressions as the answer to a calculation item.
- Every input number comes from the passage or the stem; never invent hidden
  constants, conversions, or assumptions.

Write fewer, better questions. If the passage supports fewer good questions,
return fewer. Return exactly this JSON shape. Every field below is present on
every question and every choice; use "" or [] where one does not apply. This
template is the shape, not a suggestion -- a field left out entirely is a
missing answer, not a blank one:
{"questions":[{"kind":"mcq_single","stem":"...","choices":[{"content":"...","is_correct":true,"distractor_expression":"","distractor_atom_id":""},{"content":"...","is_correct":false,"distractor_expression":"12*5.0","distractor_atom_id":""},{"content":"...","is_correct":false,"distractor_expression":"","distractor_atom_id":"A042"},{"content":"...","is_correct":false,"distractor_expression":"","distractor_atom_id":"A017"}],"explanation":"...","source_quote":"...","difficulty":"easy","skill":"recall","requires_calculation":false,"calculation":{"expression":"12/5.0","expected":2.4,"unit":"m/s^2"},"supporting_atom_ids":[],"decoy_values":[],"flawed_expression":"","reasoning_steps":[],"changed_condition":"","distractor_reasons":[]}]}
Omit the calculation object only when requires_calculation is false; when it is
true the object is mandatory and the item is rejected without it.
expression holds arithmetic and nothing else: numbers, operators, parentheses.
Write "100/5", never "a = F_net / m", "Fnet/m", "4T-f", or "100/5 m/s^2". A
symbol with no value behind it cannot be evaluated and the item is rejected —
substitute the numbers yourself and put the derivation in explanation.

An error-finding question fills the two expressions differently. The stem shows
the mistaken work and the number it produced; calculation.expression is the
arithmetic that was meant, flawed_expression is the arithmetic that was typed.
They must not be the same string:
{"stem":"A student finds the acceleration of a 5.0 kg crate under a 12 N net force by writing 12 * 5.0 = 60 m/s^2. What is wrong with this work?","skill":"error-finding","requires_calculation":true,"calculation":{"expression":"12/5.0","expected":2.4,"unit":"m/s^2"},"flawed_expression":"12*5.0","choices":[{"content":"the force was multiplied by the mass instead of divided by it","is_correct":true,"distractor_expression":"","distractor_atom_id":""}]}`

func rejectionMemoryBlock(feedback []RejectedDraft) string {
	var b strings.Builder
	b.WriteString("Rejected draft memory (negative constraints; do not repair or repeat these):\n")
	for i, draft := range feedback {
		fmt.Fprintf(&b, "%d. rejected stem: %s\n", i+1, draft.Stem)
		if len(draft.Choices) > 0 {
			fmt.Fprintf(&b, "   rejected choices: %s\n", strings.Join(draft.Choices, " | "))
		}
		for _, failure := range draft.Failures {
			fmt.Fprintf(&b, "   failed %s: %s\n", failure.Gate, failure.Reason)
		}
	}
	b.WriteString("Write a materially different question that avoids all patterns above.\n\n")
	return b.String()
}

// QualitySchema is intentionally a compact batch schema. It is an advisory
// semantic review, so it reports reasons and dimensions for inspection while
// Go computes the aggregate score and validates coverage.
func QualitySchema() map[string]any {
	verdict := obj(map[string]any{
		"question_index":       map[string]any{"type": "integer", "minimum": 0},
		"score":                map[string]any{"type": "integer", "minimum": 0, "maximum": 4},
		"source_dependency":    enum("does the answer depend on a fact or relationship specific to the supplied source", "specific", "generic", "unclear"),
		"correctness":          enum("whether the claimed answer is supported and uniquely correct", "supported", "unsupported", "ambiguous", "unclear"),
		"distractor_quality":   enum("whether the other choices are wrong rather than true or defensible", "clean", "weak", "ambiguous", "defensible"),
		"skill_difficulty_fit": enum("whether the labels honestly describe the reasoning load", "fit", "overclaimed", "mismatched", "unclear"),
		"reason":               str("at most 30 words; name the main quality issue or say sound"),
	}, "question_index", "score", "source_dependency", "correctness", "distractor_quality", "skill_difficulty_fit", "reason")
	return obj(map[string]any{
		"questions": map[string]any{"type": "array", "minItems": 1, "items": verdict},
	}, "questions")
}

const qualitySystem = `You are an advisory semantic-quality reviewer for generated exam questions.
You receive source excerpts and questions that already passed deterministic QC.
Review the question against the supplied source, not against what you happen to
know from general subject knowledge. The key test is whether the answer is tied
to a specific claim, condition, number, relationship, sequence, or example in
the source.

For each question score 0 to 4:
- 4: source-specific, answer supported and unique, distractors clean, and the
  skill/difficulty labels are honest.
- 3: usable with one minor weakness.
- 2: partly usable but generic, weakly distractored, or mislabeled.
- 1: major semantic defect that needs rewriting.
- 0: unsupported, ambiguous, or not a valid question.

Do not give credit merely because the claimed answer is a common fact. A source
quote that only repeats a textbook generality is not source-specific. For
application, require a changed condition or genuinely new situation that uses a
source relationship. For hard difficulty, require at least two linked
inferences, constraints, or transformations. Judge every question and return
exactly one verdict per input index, inside this exact top-level shape:
{"questions":[{"question_index":0,"score":4,"source_dependency":"specific","correctness":"supported","distractor_quality":"clean","skill_difficulty_fit":"fit","reason":"sound"}]}`

func QualitySystem() string { return qualitySystem }

// QualityPrompt renders only the chunks cited by the candidate questions. This
// keeps the advisory pass materially cheaper than replaying the full graph
// context while retaining enough evidence to catch generic stems and bad keys.
func QualityPrompt(lesson Lesson, chunks []Chunk, questions []Question) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lesson: %s\n\n", lesson.Title)
	b.WriteString("Source excerpts:\n")
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "[%s | page %d]\n%s\n\n", chunk.ID, chunk.Page, chunk.Text)
	}
	b.WriteString("Questions to review. The claimed correct choice is model metadata, not proof; verify it against the source.\n")
	for i, q := range questions {
		correct := -1
		for choiceIndex, choice := range q.Choices {
			if choice.IsCorrect {
				correct = choiceIndex
				break
			}
		}
		fmt.Fprintf(&b, "\nQUESTION %d\nprovenance_chunk=%s evidence_atom=%s skill=%s difficulty=%s claimed_correct_choice=%d\nsource_quote=%q\nstem=%s\nchoices:\n", i, q.EvidenceChunkID, q.EvidenceAtomID, q.Skill, q.Difficulty, correct, q.SourceQuote, q.Stem)
		for choiceIndex, choice := range q.Choices {
			fmt.Fprintf(&b, "%d. %s\n", choiceIndex, choice.Content)
		}
	}
	b.WriteString("\nReturn one verdict for every QUESTION index. Keep reason short.")
	return b.String()
}
