package generation

import (
	"context"
	"fmt"
	"strings"
)

// QuestionSlot is a set-level target, not a question. Planning these slots
// before generation gives separate generation calls a shared coverage map.
type QuestionSlot struct {
	SourceChunkID string `json:"source_chunk_id"`
	Skill         string `json:"skill"`
	Difficulty    string `json:"difficulty"`
	Focus         string `json:"focus"`
	Target        string `json:"target"`
	Scenario      string `json:"scenario"`
}

type QuestionPlan struct {
	Slots []QuestionSlot `json:"slots"`
}

// LessonPlanner is optional so existing lightweight generators and tests keep
// working. The production LLM generator implements it when plan-first mode is
// requested.
type LessonPlanner interface {
	PlanQuestions(ctx context.Context, lesson Lesson, graph *EvidenceGraph, chunks []Chunk, budget int) (QuestionPlan, error)
}

func QuestionPlanSystem() string {
	return `You are planning a coherent assessment set from source material from any subject.
Infer the domain from the source; never use a subject-specific or
teacher-edition template. Do not write questions yet. Create a compact set of
question slots that a later writer can fill.

Every slot must be supported by a specific source chunk. Across the set, vary
the concept, relationship, answer target, and scenario. Do not plan the same
fact, relationship, or scenario twice with different wording. Use application
only for a genuinely new situation or changed condition; use calculation only
when numerical work is necessary. Prefer fewer distinct slots over padding.`
}

func QuestionPlanSchema() map[string]any {
	slot := obj(map[string]any{
		"source_chunk_id": str("exact source chunk ID that supports this slot"),
		"skill":           enum("honest reasoning mode", "recall", "understanding", "application", "calculation"),
		"difficulty":      enum("honest reasoning load", "easy", "medium", "hard"),
		"focus":           str("the distinct source concept or relationship to assess"),
		"target":          str("what the student must identify, compare, predict, explain, or calculate"),
		"scenario":        str("new situation or changed condition for application; empty for recall/understanding when not needed"),
	}, "source_chunk_id", "skill", "difficulty", "focus", "target", "scenario")
	return obj(map[string]any{
		"slots": map[string]any{
			"type":     "array",
			"minItems": 1,
			"maxItems": 20,
			"items":    slot,
		},
	}, "slots")
}

func QuestionPlanPrompt(lesson Lesson, graph *EvidenceGraph, chunks []Chunk, budget int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lesson: %s\nSummary: %s\nTarget budget: %d\n\n", lesson.Title, lesson.Summary, budget)
	if graph != nil {
		var concepts []string
		for _, concept := range graph.Concepts {
			if containsString(lesson.ConceptIDs, concept.ID) {
				concepts = append(concepts, fmt.Sprintf("- %s: %s", concept.ID, concept.Title))
			}
		}
		if len(concepts) > 0 {
			b.WriteString("Lesson concepts:\n")
			b.WriteString(strings.Join(concepts, "\n"))
			b.WriteString("\n\n")
		}
	}
	b.WriteString("Source chunks (use only these as evidence):\n")
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "[%s | page %d]\n%s\n\n", chunk.ID, chunk.Page, chunk.Text)
	}
	b.WriteString("Return one slot per genuinely distinct question the source can support, up to the target budget. Do not draft stems or choices.")
	return b.String()
}
