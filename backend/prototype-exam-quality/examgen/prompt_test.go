package examgen

import (
	"strings"
	"testing"
)

func TestQuestionPromptIncludesSourceGroundedConceptFocus(t *testing.T) {
	graph := &EvidenceGraph{Concepts: []ConceptNode{
		{ID: "C001", Title: "การย่อยอาหาร", ChunkIDs: []string{"p31-c43"}, Pages: []int{31}},
		{ID: "C002", Title: "การแลกเปลี่ยนแก๊ส", ChunkIDs: []string{"p60-c90"}, Pages: []int{60}},
	}}
	lesson := Lesson{ID: "L01", Title: "ระบบย่อยอาหาร", ConceptIDs: []string{"C001", "C002"}}
	prompt := QuestionPrompt(lesson, graph, Chunk{ID: "p31-c43", Page: 31, Text: "source passage"}, 2, false)

	if !strings.Contains(prompt, "C001 | การย่อยอาหาร") {
		t.Fatalf("prompt omitted concept evidenced by this chunk:\n%s", prompt)
	}
	if strings.Contains(prompt, "การแลกเปลี่ยนแก๊ส") {
		t.Fatalf("prompt included concept with no evidence in this chunk:\n%s", prompt)
	}
	if !strings.Contains(QuestionSystem(), "asks which items comprise a set") || !strings.Contains(QuestionSystem(), "do not write that question") {
		t.Fatalf("question system does not prevent synonym-swapped list distractors:\n%s", QuestionSystem())
	}
}

func TestRepairPromptIncludesEverySemanticChoiceVerdict(t *testing.T) {
	q := Question{Stem: "Which sequence is correct?", Choices: []Choice{
		{Content: "A", IsCorrect: true}, {Content: "B"}, {Content: "A paraphrased"}, {Content: "D"},
	}, SourceQuote: "A sufficiently long source quotation for this regression test."}
	failure := GateResult{
		Gate:   GateSingleValid,
		Reason: "choice 3 is equivalent",
		ChoiceVerdicts: []ChoiceVerdict{
			{Index: 0, Status: ChoiceSupported, Reason: "directly stated"},
			{Index: 1, Status: ChoiceUnsupported, Reason: "wrong order"},
			{Index: 2, Status: ChoiceEquivalent, Reason: "same meaning as A"},
			{Index: 3, Status: ChoiceAmbiguous, Reason: "could be read two ways"},
		},
	}

	prompt := RepairPrompt(q, Chunk{Page: 1, Text: q.SourceQuote}, []GateResult{failure})
	for _, want := range []string{"choice 1: supported", "choice 2: unsupported", "choice 3: equivalent", "choice 4: ambiguous", "same meaning as A"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("repair prompt omitted %q:\n%s", want, prompt)
		}
	}
	for _, want := range []string{`MANDATORY REPLACEMENT choice 3: do not return "A paraphrased"`, `MANDATORY REPLACEMENT choice 4: do not return "D"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("repair prompt omitted %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(RepairSystem(), "Do not merely move the correct-answer marker") {
		t.Fatalf("repair system does not prevent answer-key swapping:\n%s", RepairSystem())
	}
	if !strings.Contains(RepairSystem(), `{"questions":[{"kind":"mcq_single"`) || !strings.Contains(RepairSystem(), `"content":"...","is_correct":true`) {
		t.Fatalf("repair system omits the exact JSON contract needed by JSON-mode providers:\n%s", RepairSystem())
	}
	if !strings.Contains(RepairSystem(), "rewrite all three distractors") || !strings.Contains(RepairSystem(), "different source fact") {
		t.Fatalf("repair system only fixes the reported distractor instead of making the full option set robust:\n%s", RepairSystem())
	}
	if !strings.Contains(prompt, "This failure is repairable. Return exactly one repaired question") {
		t.Fatalf("repair prompt permits an empty semantic repair:\n%s", prompt)
	}
}

func TestDistractorRepairPromptIsAFocusedReplacementContract(t *testing.T) {
	q := Question{Stem: "Which sequence is correct?", Choices: []Choice{
		{Content: "A then B then C", IsCorrect: true},
		{Content: "A paraphrased"},
		{Content: "A then C then B"},
		{Content: "A broader"},
	}}
	verdicts := []ChoiceVerdict{
		{Index: 0, Status: ChoiceSupported, Reason: "direct"},
		{Index: 1, Status: ChoiceEquivalent, Reason: "same meaning"},
		{Index: 2, Status: ChoiceUnsupported, Reason: "wrong order"},
		{Index: 3, Status: ChoiceAmbiguous, Reason: "too broad"},
	}
	prompt := DistractorRepairPrompt(q, Chunk{Page: 4, Text: "A then B then C is the supported sequence."}, verdicts)

	for _, want := range []string{"Correct answer (preserve exactly):", "A then B then C", `FORBIDDEN 1: "A paraphrased"`, `FORBIDDEN 3: "A broader"`, "Return replacements for indices 1, 2, 3"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("focused distractor prompt omitted %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(DistractorRepairSystem(), `{"replacements":[{"index":1,"content":"..."}]}`) {
		t.Fatalf("focused distractor system omitted exact JSON contract:\n%s", DistractorRepairSystem())
	}
	if !strings.Contains(DistractorRepairSystem(), "Do not replace one listed item with a synonym") {
		t.Fatalf("focused distractor system permits synonym substitution in set questions:\n%s", DistractorRepairSystem())
	}
}
