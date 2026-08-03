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

const topicSystem = `You are indexing a textbook so it can be split into lessons.

Read the passage, say what it is, and name the topics it covers. Rules:
- Name only topics the passage actually covers. Do not infer topics from a
  heading that has no material under it.
- Write the titles in the same language as the passage.
- A title is a noun phrase, not a sentence, and not a question.

Then set kind — one judgement about the whole passage, not about each topic.
Many textbooks are teacher's editions, so a passage can be well-written prose
and still teach the learner nothing:

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

Judge what the passage IS, not what it is about. A page of answers to chapter
exercises is apparatus even though every answer is about the subject. A
laboratory activity that explains what happens and why is content even though a
teacher runs it.

Most of this book is a teacher's edition, so subject matter usually arrives
wrapped in instructions to the teacher: "the teacher should explain that...",
"have the students search for...", "ครูควรชี้แจงว่า...", "ครูให้นักเรียนอธิบาย...".
That wrapper is not what the passage is. If the passage explains the subject
anywhere inside it — states a mechanism, names a structure and its function,
gives data or a reason — it is content, no matter who is being addressed. Mark
apparatus only when the passage would still say nothing about the subject after
you removed the instructions: a bare list of answers, a scoring table, a
checklist of objectives, a schedule.

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
		"expression": str("the arithmetic to solve this question, as a plain expression using only numbers, + - * / ( ) and decimal points. No variables, no functions, no units. Example: (1200*0.07)/12"),
		"expected":   map[string]any{"type": "number", "description": "the value that expression produces"},
	}, "expression", "expected")

	question := map[string]any{
		"kind":         enum("question shape", string(KindMCQSingle)),
		"stem":         str("the question itself, understandable on its own without seeing the passage"),
		"explanation":  str("why the correct choice is correct, in the source language"),
		"source_quote": str("a sentence copied CHARACTER FOR CHARACTER from the passage that makes the correct answer true. Must appear in the passage exactly."),
		"difficulty":   enum("how hard this is for someone who has read the passage once", "easy", "medium", "hard"),
		"skill":        enum("what the question tests", "recall", "understanding", "application", "calculation"),
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

const questionSystem = `You write multiple-choice exam questions from a passage of a textbook.

The single most important rule: a student must be able to read your question,
understand exactly what is being asked, and answer it — without the passage in
front of them and without guessing what you meant.

Hard requirements for every question:
- The stem stands alone. Never write "according to the passage", "in the text
  above", "this value", "the diagram", or anything that points at material the
  student cannot see. Name the thing you are asking about.
- Exactly one choice is correct. The other three must be wrong for a reason a
  student could articulate — not wrong because they are nonsense.
- A distractor must not be a synonym, paraphrase, ordinary-language restatement,
  or merely broader/narrower wording of the correct answer. If two choices could
  mean the same thing under a reasonable interpretation, rewrite one of them.
- If the stem asks which items comprise a set, each distractor must replace at
  least one item with a clearly different, source-contradicted process or
  entity. Do not swap an item for a near-synonym. If the passage cannot support
  three unambiguous false sets, do not write that question.
- When the user message includes rejected draft memory, treat it as a negative
  constraint. Do not repair, paraphrase, or lightly vary those drafts. Ask a
  materially different question and avoid every failure pattern listed there.
- The answer must depend on something this passage actually says. Anchor it to
  a number, a named structure, an order of steps, a condition, or a stated
  cause-and-effect that appears in the passage. A student who knows the subject
  in general, but has not read this passage, should not be able to answer
  reliably. Before writing a question, ask yourself whether a good student could
  answer it from general knowledge alone; if so, do not write it — find a
  specific the passage supplies and ask about that instead.
- Ask only about subject matter. Never ask about learning objectives,
  assessment guidelines or criteria, classroom/teaching activities, videos,
  tests, pedagogy, or chapter numbering. Those are document metadata, even when
  the passage explicitly states them. If a passage contains only teacher
  guidance, return no questions.
- All four choices are the same kind of thing and roughly the same length. Do
  not make the correct answer the longest or the most detailed.
- "All of the above", "none of the above", and "both A and B" are forbidden.
- source_quote is copied character for character from the passage. Do not
  paraphrase it, do not fix its punctuation, do not translate it. If nothing in
  the passage supports your question, do not write the question.
- Write in the same language as the passage.

For calculation questions:
- Do not do the arithmetic yourself. Put the arithmetic in
  calculation.expression as a plain expression and put the result you believe
  it produces in calculation.expected. The expression will be evaluated
  independently and your question is discarded if it disagrees.
- calculation.expected must be a bare JSON number — not a quoted string and
  not a number with a unit.
- The correct choice must contain that computed number.
- Every number in the expression must come from the passage or from the stem.
  Do not invent inputs.

Write fewer, better questions. A passage that supports two good questions gets
two questions, not six padded ones.

Return exactly this JSON shape. Do not use a field named "question" instead of
"stem", do not use "correct_index", and do not make choices plain strings:
{"questions":[{"kind":"mcq_single","stem":"...","choices":[{"content":"...","is_correct":true},{"content":"...","is_correct":false},{"content":"...","is_correct":false},{"content":"...","is_correct":false}],"explanation":"...","source_quote":"...","difficulty":"easy","skill":"recall"}]}`

func QuestionSystem() string { return questionSystem }

// QuestionPrompt builds the generation message.
func QuestionPrompt(lesson Lesson, graph *EvidenceGraph, c Chunk, feedback []RejectedDraft, want int, forceCalc bool) string {
	s := fmt.Sprintf("Lesson: %s\n\n", lesson.Title)
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
		"guessed_index": map[string]any{
			"type":        "integer",
			"minimum":     0,
			"maximum":     numChoices - 1,
			"description": "which choice you would pick, 0-based, guessing if you must",
		},
		"guess_confidence": enum("how sure you are of that guess without any source material", "low", "medium", "high"),
	}, "interpretable", "reason", "guessed_index", "guess_confidence")
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

Then, separately, guess which choice is correct and say how confident you are.
A high-confidence guess with no source material is worth knowing about.`

func BlindSystem() string { return blindSystem }

// SourcedSchema constrains the judge that can read the source.
func SourcedSchema(numChoices int) map[string]any {
	return obj(map[string]any{
		"choice_verdicts": map[string]any{
			"type":        "array",
			"minItems":    numChoices,
			"maxItems":    numChoices,
			"description": "one semantic audit for every choice, including the best choice",
			"items": obj(map[string]any{
				"index": map[string]any{
					"type":    "integer",
					"minimum": 0,
					"maximum": numChoices - 1,
				},
				"status": enum("semantic status against the passage and the other choices", "supported", "unsupported", "equivalent", "ambiguous"),
				"reason": str("at most 15 words explaining this choice specifically"),
			}, "index", "status", "reason"),
		},
	}, "choice_verdicts")
}

const sourcedSystem = `You are checking an exam question against the passage it was written from.

Do one thing: audit EVERY choice separately against the passage and the other
choices. Mark the best answer "supported". Mark a
   distractor "equivalent" when it is a synonym, paraphrase, broader/narrower
   wording, or ordinary-language restatement of the supported answer. Mark it
   "ambiguous" when a reasonable interpretation could make it true. Only mark
   it "unsupported" when the passage and ordinary meaning clearly rule it out.

Do not excuse equivalent wording because the textbook uses a more technical
term. For example, an option saying "defecation" and another saying "passing
stool" can be equivalent even if only one phrase appears verbatim.

Return exactly one top-level field named choice_verdicts. It must contain one
object for every choice index. Do not return best_index or also_defensible; the
caller derives them from your per-choice statuses. You are not told which choice
the author marked correct. Judge only against the passage.`

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
