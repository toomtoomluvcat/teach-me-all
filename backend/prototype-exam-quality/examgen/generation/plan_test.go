package generation

import (
	"strings"
	"testing"
)

func TestQuestionPlanPromptIsDomainAgnosticAndSourceBound(t *testing.T) {
	planPrompt := QuestionPlanPrompt(
		Lesson{Title: "Circular motion", Summary: "relationships among speed, radius, and force", QuestionBudget: 3},
		&EvidenceGraph{Concepts: []ConceptNode{{ID: "C1", Title: "centripetal acceleration", ChunkIDs: []string{"p1-c1"}}}},
		[]Chunk{{ID: "p1-c1", Page: 1, Text: "Acceleration points toward the center."}},
		3,
	)
	for _, want := range []string{"any subject", "specific source chunk", "vary", "p1-c1", "Do not draft stems"} {
		if !strings.Contains(planPrompt+QuestionPlanSystem(), want) {
			t.Fatalf("question plan omitted %q:\n%s", want, planPrompt)
		}
	}
	props := QuestionPlanSchema()["properties"].(map[string]any)
	if _, ok := props["slots"]; !ok {
		t.Fatalf("plan schema missing slots: %#v", props)
	}
}

func TestQuestionPlanDirectiveCarriesCurrentAndAcceptedTargets(t *testing.T) {
	plan := &QuestionPlan{Slots: []QuestionSlot{
		{SourceChunkID: "p1-c1", Skill: "recall", Difficulty: "easy", Focus: "direction", Target: "identify"},
		{SourceChunkID: "p2-c2", Skill: "application", Difficulty: "medium", Focus: "speed-radius relation", Target: "predict", Scenario: "double the speed"},
	}}
	directive := questionPlanDirective(plan, 1, []Question{{Stem: "Which direction does acceleration point?"}})
	for _, want := range []string{"CURRENT", "speed-radius relation", "double the speed", "Already accepted targets", "Which direction"} {
		if !strings.Contains(directive, want) {
			t.Fatalf("plan directive omitted %q:\n%s", want, directive)
		}
	}
}
