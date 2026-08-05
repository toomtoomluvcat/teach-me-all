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
	blindProps := BlindSchema(4)["properties"].(map[string]any)
	if _, ok := blindProps["guessed_index"]; ok {
		t.Fatal("blind schema still asks the model to guess the answer")
	}
	for _, want := range []string{"dependency", "evidence"} {
		if !strings.Contains(SourcedSystem(), want) {
			t.Fatalf("source judge prompt does not request %q:\n%s", want, SourcedSystem())
		}
	}
	for _, unwanted := range []string{"choice_verdicts", "dependency_kind", "counterfactual", "dependency_reason"} {
		if strings.Contains(SourcedSystem(), unwanted) {
			t.Fatalf("source judge prompt still requests deferred field %q:\n%s", unwanted, SourcedSystem())
		}
	}
}

func TestQuestionSystemIsDomainAgnostic(t *testing.T) {
	system := QuestionSystem()
	for _, want := range []string{
		"domain-agnostic",
		"science, mathematics, medicine",
		"subject-specific template",
		"genuinely new situation",
		"name,",
		"at least two linked inferences",
		"same scenario or principle twice",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("domain-agnostic contract omitted %q:\n%s", want, system)
		}
	}
	for _, unwanted := range []string{"physics", "newton", "m/s^2", "physical unit"} {
		if strings.Contains(strings.ToLower(system), unwanted) {
			t.Fatalf("question system contains subject-specific prompt bias %q:\n%s", unwanted, system)
		}
	}
}

func TestTopicSystemDoesNotAssumeBiologyOrTeacherEdition(t *testing.T) {
	system := TopicSystem()
	for _, want := range []string{"student textbook, reference work, teacher guide", "any subject", "Do not assume"} {
		if !strings.Contains(system, want) {
			t.Fatalf("topic classifier omitted dynamic-source rule %q:\n%s", want, system)
		}
	}
	for _, unwanted := range []string{"Most of this book is a teacher's edition", "ครูควรชี้แจงว่า"} {
		if strings.Contains(system, unwanted) {
			t.Fatalf("topic classifier still contains source-specific assumption %q:\n%s", unwanted, system)
		}
	}
}

func TestSourcedSchemaIsMinimal(t *testing.T) {
	properties := SourcedSchema()["properties"].(map[string]any)
	for _, want := range []string{"dependency", "evidence"} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("minimal source schema omitted %q: %#v", want, properties)
		}
	}
	for _, unwanted := range []string{"choice_verdicts", "source_dependency", "dependency_kind", "counterfactual", "dependency_reason"} {
		if _, ok := properties[unwanted]; ok {
			t.Fatalf("minimal source schema still exposes deferred field %q", unwanted)
		}
	}
}

func TestBlindSystemDoesNotCoachConfidence(t *testing.T) {
	if strings.Contains(BlindSystem(), "Do not be modest") {
		t.Fatal("blind judge prompt still coaches the confidence distribution")
	}
	if strings.Contains(BlindSystem(), "guess_confidence") || strings.Contains(BlindSystem(), "guessed_index") {
		t.Fatal("blind judge prompt still asks the model to guess the answer")
	}
}

func TestQuestionPromptCarriesSemanticFailureAsNegativeMemory(t *testing.T) {
	feedback := []RejectedDraft{{
		Stem:    "Which list is correct?",
		Choices: []string{"A", "B", "A paraphrased", "D"},
		Failures: []GateResult{{
			Gate: GateSingleValid, Reason: "choice 3 also defensible",
			ChoiceVerdicts: []ChoiceVerdict{
				{Index: 0, Status: ChoiceSupported, Reason: "direct"},
				{Index: 1, Status: ChoiceUnsupported, Reason: "wrong"},
				{Index: 2, Status: ChoiceEquivalent, Reason: "same meaning as A"},
				{Index: 3, Status: ChoiceUnsupported, Reason: "wrong"},
			},
		}},
	}}
	prompt := QuestionPrompt(Lesson{Title: "Lesson"}, nil, Chunk{Page: 2, Text: "new source"}, feedback, 1, false)
	for _, want := range []string{"Rejected draft memory", "Which list is correct?", "A paraphrased", "choice 3 was equivalent", "same meaning as A", "materially different question"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("generation prompt omitted %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "choice 2 was unsupported") {
		t.Fatalf("generation prompt wasted tokens on already-invalid distractors:\n%s", prompt)
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
