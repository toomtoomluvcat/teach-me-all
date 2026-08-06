package generation

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
	prompt := QuestionPrompt(lesson, graph, Chunk{ID: "p31-c43", Page: 31, Text: "source passage"}, nil, 2, false)

	if !strings.Contains(prompt, "C001 | การย่อยอาหาร") {
		t.Fatalf("prompt omitted concept evidenced by this chunk:\n%s", prompt)
	}
	if strings.Contains(prompt, "การแลกเปลี่ยนแก๊ส") {
		t.Fatalf("prompt included concept with no evidence in this chunk:\n%s", prompt)
	}
	if !strings.Contains(QuestionSystem(), "choices that merely restate the stem") {
		t.Fatalf("question system does not prevent restated distractors:\n%s", QuestionSystem())
	}
	for _, want := range []string{"learning objectives", "assessment rules", "numbering"} {
		if !strings.Contains(QuestionSystem(), want) {
			t.Fatalf("question system does not ban teacher-guide metadata %q:\n%s", want, QuestionSystem())
		}
	}
	if !strings.Contains(QuestionSystem(), "pre-learning checks") || !strings.Contains(QuestionSystem(), "answer keys") {
		t.Fatalf("question system does not restrict evidence to core text:\n%s", QuestionSystem())
	}
}

func TestQuestionPromptCarriesGateFailureAsNegativeMemory(t *testing.T) {
	feedback := []RejectedDraft{{
		Stem:     "Which list is correct?",
		Choices:  []string{"A", "B", "A paraphrased", "D"},
		Failures: []GateResult{{Gate: GateCoverage, Reason: "slot S2 was already used"}},
	}}
	prompt := QuestionPrompt(Lesson{Title: "Lesson"}, nil, Chunk{Page: 2, Text: "new source"}, feedback, 1, false)
	for _, want := range []string{"Rejected draft memory", "Which list is correct?", "A paraphrased", "coverage_contract", "slot S2 was already used", "materially different question"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("generation prompt omitted %q:\n%s", want, prompt)
		}
	}
}

func TestQuestionPromptCarriesBenchmarkDirective(t *testing.T) {
	prompt := QuestionPrompt(Lesson{Title: "Projectile Motion"}, nil, Chunk{
		Page:                182,
		Text:                "A projectile is launched at an angle.",
		GenerationDirective: "Generate an easy application question.",
	}, nil, 1, false)
	if !strings.Contains(prompt, "Target for this generation call (follow exactly if the passage supports it)") ||
		!strings.Contains(prompt, "Generate an easy application question.") {
		t.Fatalf("benchmark directive missing from question prompt:\n%s", prompt)
	}
}

func TestQuestionSetSchemaForContractRestrictsProvenanceIDs(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{
		{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}},
		{ID: "S02", AtomID: "A002", SourceChunkIDs: []string{"c2"}},
	}}
	root := QuestionSetSchemaForContract(false, contract)["properties"].(map[string]any)
	item := root["questions"].(map[string]any)["items"].(map[string]any)
	properties := item["properties"].(map[string]any)
	for _, test := range []struct {
		name string
		want []any
	}{
		{name: "coverage_slot_id", want: []any{"S01", "S02"}},
		{name: "evidence_atom_id", want: []any{"A001", "A002"}},
		{name: "evidence_chunk_id", want: []any{"c1", "c2"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			property := properties[test.name].(map[string]any)
			got, ok := property["enum"].([]any)
			if !ok || len(got) != len(test.want) {
				t.Fatalf("schema property = %#v, want enum %#v", property, test.want)
			}
			for i := range test.want {
				if got[i] != test.want[i] {
					t.Fatalf("enum = %#v, want %#v", got, test.want)
				}
			}
		})
	}
	if _, ok := properties["operation"]; !ok {
		t.Fatal("set schema omitted the contract operation field")
	}
	if _, ok := properties["requires_calculation"]; !ok {
		t.Fatal("set schema omitted the calculation flag")
	}
	questionSkill := properties["skill"].(map[string]any)
	for _, value := range questionSkill["enum"].([]any) {
		if value == "calculation" {
			t.Fatal("calculation leaked back into the skill enum")
		}
	}
	operation := properties["operation"].(map[string]any)
	gotOperations, ok := operation["enum"].([]any)
	if ok && len(gotOperations) != 0 {
		t.Fatalf("operation schema should stay open only when contract has no operations: %#v", operation)
	}
	withOperation := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", Operation: "causal"}, {ID: "S02", AtomID: "A002", Operation: "comparison"}}}
	withOperationRoot := QuestionSetSchemaForContract(false, withOperation)["properties"].(map[string]any)
	withOperationItem := withOperationRoot["questions"].(map[string]any)["items"].(map[string]any)
	withOperationSchema := withOperationItem["properties"].(map[string]any)["operation"].(map[string]any)
	values, ok := withOperationSchema["enum"].([]any)
	if !ok || len(values) != 2 || values[0] != "causal" || values[1] != "comparison" {
		t.Fatalf("operation enum = %#v, want causal/comparison", withOperationSchema)
	}
	prompt := QuestionSetPrompt(Lesson{Title: "Lesson"}, nil, nil, contract, nil, false)
	for _, want := range []string{"Slot execution protocol", "never invent or mix IDs", "source_quote is verbatim"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("set prompt omitted contract adherence rule %q:\n%s", want, prompt)
		}
	}
}
