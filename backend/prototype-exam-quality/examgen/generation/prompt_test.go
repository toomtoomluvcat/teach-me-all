package generation

import (
	"strings"
	"testing"
)

func TestQuestionSetPromptKeepsTheEvidencePacketSlotLocal(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A1", ChunkID: "p31-c43", ConceptIDs: []string{"C001"}, Claim: "digestion begins in the mouth"},
		{ID: "A2", ChunkID: "p60-c90", ConceptIDs: []string{"C002"}, Claim: "gas exchange happens in the alveoli"},
	}}
	contract := CoverageContract{Budget: 1, Slots: []CoverageSlot{
		{ID: "S1", AtomID: "A1", SourceChunkIDs: []string{"p31-c43"}, Skill: "understanding", Difficulty: "easy"},
	}}
	prompt := QuestionSetPrompt(Lesson{ID: "L01", Title: "Digestive system"}, graph,
		[]Chunk{{ID: "p31-c43", Page: 31, Text: "source passage"}}, contract, nil, false)

	if !strings.Contains(prompt, "digestion begins in the mouth") {
		t.Fatalf("prompt omitted the atom assigned to the slot:\n%s", prompt)
	}
	if strings.Contains(prompt, "gas exchange") {
		t.Fatalf("prompt included an atom no slot asked for:\n%s", prompt)
	}
	if !strings.Contains(QuestionSetSystem(), "choices that merely restate the stem") {
		t.Fatalf("question system does not prevent restated distractors:\n%s", QuestionSetSystem())
	}
	for _, want := range []string{"learning objectives", "assessment rules", "numbering", "pre-learning checks", "answer keys"} {
		if !strings.Contains(QuestionSetSystem(), want) {
			t.Fatalf("question system does not ban teacher-guide metadata %q:\n%s", want, QuestionSetSystem())
		}
	}
}

func TestQuestionSetPromptCarriesGateFailureAsNegativeMemory(t *testing.T) {
	feedback := []RejectedDraft{{
		Stem:     "Which list is correct?",
		Choices:  []string{"A", "B", "A paraphrased", "D"},
		Failures: []GateResult{{Gate: GateCoverage, Reason: "slot S2 was already used"}},
	}}
	prompt := QuestionSetPrompt(Lesson{Title: "Lesson"}, nil,
		[]Chunk{{ID: "c1", Page: 2, Text: "new source"}}, CoverageContract{Budget: 1}, feedback, false)
	for _, want := range []string{"Rejected draft memory", "Which list is correct?", "A paraphrased", "coverage_contract", "slot S2 was already used", "materially different question"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("generation prompt omitted %q:\n%s", want, prompt)
		}
	}
}

func TestQuestionSetPromptCarriesBenchmarkDirective(t *testing.T) {
	contract := CoverageContract{Budget: 1, GenerationDirective: "Generate an easy application question."}
	prompt := QuestionSetPrompt(Lesson{Title: "Projectile Motion"}, nil,
		[]Chunk{{ID: "c1", Page: 182, Text: "A projectile is launched at an angle."}}, contract, nil, false)
	if !strings.Contains(prompt, "Benchmark/Run directive") || !strings.Contains(prompt, "Generate an easy application question.") {
		t.Fatalf("set prompt dropped the run directive:\n%s", prompt)
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

// The source context is the byte-identical block across candidates and cases
// for a lesson, so it must precede every per-case block (directive, coverage
// contract) and every per-candidate block (the candidate marker) to stay in
// the provider's prompt-cache prefix.
func TestQuestionSetPromptOrdersSourceContextForCaching(t *testing.T) {
	contract := CoverageContract{Budget: 1, Variant: 2, Slots: []CoverageSlot{
		{ID: "S1", AtomID: "A1", SourceChunkIDs: []string{"p31-c43"}, Skill: "understanding", Difficulty: "easy"},
	}}
	prompt := QuestionSetPrompt(Lesson{ID: "L01", Title: "Digestive system"}, nil,
		[]Chunk{{ID: "p31-c43", Page: 31, Text: "source passage"}}, contract, nil, false)

	sourceAt := strings.Index(prompt, "Source context")
	contractAt := strings.Index(prompt, "Coverage contract")
	markerAt := strings.Index(prompt, "This is candidate set 2")
	if sourceAt < 0 || contractAt < 0 || markerAt < 0 {
		t.Fatalf("prompt is missing a required block:\n%s", prompt)
	}
	if !(sourceAt < contractAt && contractAt < markerAt) {
		t.Fatalf("expected Source context before Coverage contract before candidate marker, got positions %d, %d, %d:\n%s",
			sourceAt, contractAt, markerAt, prompt)
	}
}
