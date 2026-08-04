package examgen

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

// --- pass 2: question generation --------------------------------------------

// QuestionSchema constrains generation. forceCalc drops the non-calculation
// escape hatch, implementing the --force-calc flag at the schema level rather
// than by asking nicely in the prompt.
func QuestionSchema(forceCalc bool) map[string]any {
	calc := obj(map[string]any{
		"expression": str("the arithmetic to solve this question, as a plain expression using numbers, + - * / ^, parentheses, decimal points, pi, and only these functions: sin, cos, tan, sqrt, abs, exp, ln. No variables or units. Trigonometric arguments are in radians. Example: (1200*0.07)/12 or 20*sin(30*pi/180)"),
		"expected":   map[string]any{"type": "number", "description": "the value that expression produces"},
	}, "expression", "expected")

	question := map[string]any{
		"kind":         enum("question shape", string(KindMCQSingle)),
		"stem":         str("the question itself, understandable on its own without seeing the passage"),
		"explanation":  str("why the correct choice is correct, in the source language"),
		"source_quote": str("an exact contiguous substring copied CHARACTER FOR CHARACTER from the passage that makes the correct answer true; prefer a complete sentence, but a shorter exact span is allowed. Must appear exactly."),
		"difficulty":   enum("honest reasoning load: easy=one direct inference or operation, medium=one relationship with a meaningful distinction, hard=two linked inferences or competing constraints", "easy", "medium", "hard"),
		"skill":        enum("honest reasoning mode: recall=fact, understanding=interpretation, application=new situation using a source relationship, calculation=numerical computation", "recall", "understanding", "application", "calculation"),
		"choices": map[string]any{
			"type":     "array",
			"minItems": 4,
			"maxItems": 4,
			"items": obj(map[string]any{
				"content":    str("the choice text"),
				"is_correct": map[string]any{"type": "boolean"},
			}, "content", "is_correct"),
		},
	}

	required := []string{"kind", "stem", "choices", "explanation", "source_quote", "difficulty", "skill"}
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
units, and relationships from the supplied passage. Never apply a biology-only
template or import facts, vocabulary, or question patterns from a different
subject.

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
  definition, force, component, label, or unchanged property is recall or
  understanding, not application. A stem that only asks "what is", "which is",
  or repeats a source example is not application unless the student must use the
  relationship to decide something new.
- calculation: compute a numerical result. Use this only when the arithmetic is
  necessary to answer.
- easy: one direct inference or operation; medium: one relationship plus a
  meaningful distinction; hard: at least two linked inferences, constraints,
  or transformations. For easy application, change a condition or value from
  the source and ask for an outcome, not a named fact. For hard application,
  use at least two given inputs or conditions and make the student carry out at
  least two linked transformations or decisions. If one sentence of the source
  or one unchanged relationship answers it, it is not hard.

Hard requirements for every question:
- The stem stands alone. Never point at invisible material with phrases such as
  "according to the passage", "the text above", "this value", or "the diagram".
- Exactly one choice is correct. The other three must be plausible and wrong for
  a reason a student could articulate. Keep choices parallel in type and length.
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
For hard application, the explanation must expose at least two linked steps.

For calculation questions:
	- Put the arithmetic in calculation.expression using numbers, + - * / ^,
	  parentheses, decimal points, pi, and only these functions: sin, cos, tan,
	  sqrt, abs, exp, ln. Trigonometric arguments are in radians. No variables,
	  units, or other functions. The expression is checked independently.
- calculation.expected is a bare JSON number, not a string and not a unit.
- The correct choice contains the computed number and unit when a unit exists.
- Every input number comes from the passage or the stem; never invent hidden
  constants, conversions, or assumptions.

Write fewer, better questions. If the passage supports fewer good questions,
return fewer. Return exactly this JSON shape:
{"questions":[{"kind":"mcq_single","stem":"...","choices":[{"content":"...","is_correct":true},{"content":"...","is_correct":false},{"content":"...","is_correct":false},{"content":"...","is_correct":false}],"explanation":"...","source_quote":"...","difficulty":"easy","skill":"recall"}]}`

func QuestionSystem() string { return questionSystem }

// QuestionPrompt builds the generation message.
func QuestionPrompt(lesson Lesson, graph *EvidenceGraph, c Chunk, feedback []RejectedDraft, want int, forceCalc bool) string {
	s := fmt.Sprintf("Lesson: %s\n\n", lesson.Title)
	s += "The subject and domain are inferred from this passage. Use no preselected subject template.\n\n"
	if graph != nil {
		var focus []string
		for _, concept := range graph.Concepts {
			if containsString(lesson.ConceptIDs, concept.ID) && containsString(concept.ChunkIDs, c.ID) {
				focus = append(focus, fmt.Sprintf("- %s | %s", concept.ID, concept.Title))
			}
		}
		if len(focus) > 0 {
			s += "Concepts evidenced by this passage:\n" + strings.Join(focus, "\n") + "\n\n"
		}
	}
	if directive := strings.TrimSpace(c.GenerationDirective); directive != "" {
		s += "Target for this generation call (follow exactly if the passage supports it): " + directive + "\n\n"
	}
	s += fmt.Sprintf("Passage (page %d):\n\n%s\n\n", c.Page, c.Text)
	if len(feedback) > 0 {
		s += rejectionMemoryBlock(feedback)
	}
	s += fmt.Sprintf("Write up to %d question(s) from this passage. ", want)
	s += "If the passage only supports fewer, write fewer. "
	if forceCalc {
		s += "Every question must be a calculation question with a calculation expression. " +
			"If this passage contains no numbers to compute with, return an empty list."
	} else {
		s += "Include a calculation only when the passage contains numbers worth computing with."
	}
	return s
}

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
			for _, verdict := range failure.ChoiceVerdicts {
				if verdict.Status == ChoiceUnsupported {
					continue
				}
				fmt.Fprintf(&b, "     choice %d was %s: %s\n", verdict.Index+1, verdict.Status, verdict.Reason)
			}
		}
	}
	b.WriteString("Write a materially different question that avoids all patterns above.\n\n")
	return b.String()
}

// --- gates 2 and 3 ----------------------------------------------------------

// BlindSchema constrains the judge that sees no source.
func BlindSchema(numChoices int) map[string]any {
	return obj(map[string]any{
		"interpretable": map[string]any{
			"type":        "boolean",
			"description": "true if it is clear what the question is asking",
		},
		"reason": str("at most 20 words. If not interpretable, say exactly what is missing or ambiguous. Do not restate the question and do not solve it."),
	}, "interpretable", "reason")
}

const blindSystem = `You are checking whether an exam question is written clearly. You cannot see
the material it came from, and that is intentional.

Decide one thing: reading only the question and its choices, is it clear what is
being asked?

Mark it NOT interpretable when the question:
- refers to something you cannot see ("the passage", "the figure above", "this
  process", "the value given")
- is so general that several different answers would all be reasonable
- is missing information needed to even understand the task
- asks about "it" or "they" without ever saying what

Mark it interpretable when a knowledgeable person would understand the task,
even if they would need to have studied the material to know the answer. Needing
knowledge is fine. Needing context you were not given is not.

Return exactly this JSON shape, with no other top-level keys:
{"interpretable":true,"reason":"..."}`

func BlindSystem() string { return blindSystem }

// SourcedSchema constrains the minimal source-dependency judge. The variadic
// argument keeps old callers source-compatible; the number of choices is no
// longer part of this contract because semantic choice auditing is deferred.
func SourcedSchema(_ ...int) map[string]any {
	return obj(map[string]any{
		"dependency": enum("whether the best answer requires a fact specific to this passage, not general subject knowledge", "specific", "generic", "unclear"),
		"evidence":   str("one exact substring from the passage that makes the answer specific; empty for generic or unclear"),
	}, "dependency", "evidence")
}

const sourcedSystem = `You are checking an exam question against the passage it was written from.

Do not ask whether you personally knew the answer before seeing the passage.
Ask whether a learner needs a fact or relationship that THIS passage specifically
supplies.

Use "specific" only when all of these are true:
- the answer depends on a particular fact or relationship supplied by this passage;
- the fact is present in the passage, not merely implied by the topic;
- without that fact, the best choice could not be identified from general knowledge.

Use "generic" when the answer follows from general subject knowledge, the
wording of the question and choices, or a standard principle even without this
passage. A citation that merely repeats a general fact is not enough. Use
"unclear" when you cannot decide. For "specific", return one shortest exact
substring from the passage. For "generic" or "unclear", return an empty string.

Return exactly this JSON object and no other fields:
{"dependency":"specific|generic|unclear","evidence":"exact passage substring or empty string"}`

func SourcedSystem() string { return sourcedSystem }

// BlindPrompt renders a question with its source deliberately withheld.
func BlindPrompt(q Question) string {
	s := "Question: " + q.Stem + "\n\nChoices:\n"
	for i, c := range q.Choices {
		s += fmt.Sprintf("%d. %s\n", i, c.Content)
	}
	return s
}

// SourcedPrompt renders a question together with the chunk it came from.
func SourcedPrompt(q Question, source string) string {
	s := "Passage:\n\n" + source + "\n\nQuestion: " + q.Stem + "\n\nChoices:\n"
	for i, c := range q.Choices {
		s += fmt.Sprintf("%d. %s\n", i, c.Content)
	}
	return s
}
