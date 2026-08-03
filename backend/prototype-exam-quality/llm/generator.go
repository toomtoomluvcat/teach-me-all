package llm

import (
	"context"
	"fmt"

	"protoexam/examgen"
)

// Generator adapts the Ollama client to examgen.Generator.
type Generator struct {
	c     ModelClient
	model string
	// UseCalcTool runs the calculator tool loop before generating, so the model
	// never has to do arithmetic in its head.
	UseCalcTool bool
}

func NewGenerator(c ModelClient, model string) *Generator { return &Generator{c: c, model: model} }

// genOptions keeps generation close to deterministic. A prototype whose output
// changes every run cannot be compared against itself between prompt edits.
func genOptions(numCtx int, temp float64) *Options {
	return &Options{
		NumCtx:        numCtx,
		Temperature:   temp,
		TopP:          0.9,
		RepeatPenalty: 1.1,
		Seed:          1,
	}
}

func (g *Generator) Topics(ctx context.Context, c examgen.Chunk) ([]examgen.Topic, error) {
	ctx = WithLabel(ctx, "outline/map")
	var out struct {
		Topics []examgen.Topic `json:"topics"`
	}
	msgs := []Message{
		{Role: "system", Content: examgen.TopicSystem()},
		{Role: "user", Content: examgen.TopicPrompt(c)},
	}
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.TopicSchema(), genOptions(4096, 0), &out); err != nil {
		return nil, err
	}
	return out.Topics, nil
}

func (g *Generator) Outline(ctx context.Context, graph examgen.EvidenceGraph) (*examgen.Outline, []examgen.LessonConcepts, error) {
	ctx = WithLabel(ctx, "outline/reduce")
	var out struct {
		CourseTitle string `json:"course_title"`
		Lessons     []struct {
			Title          string   `json:"title"`
			Summary        string   `json:"summary"`
			QuestionBudget int      `json:"question_budget"`
			ConceptIDs     []string `json:"concept_ids"`
		} `json:"lessons"`
	}

	msgs := []Message{
		{Role: "system", Content: examgen.OutlineSystem()},
		{Role: "user", Content: examgen.OutlinePrompt(graph)},
	}
	// The reduce step sees every topic at once, so it needs a bigger window
	// than the per-chunk calls.
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.OutlineSchema(), genOptions(16384, 0), &out); err != nil {
		return nil, nil, err
	}

	outline := &examgen.Outline{CourseTitle: out.CourseTitle}
	var membership []examgen.LessonConcepts
	for i, l := range out.Lessons {
		id := fmt.Sprintf("L%02d", i+1)
		outline.Lessons = append(outline.Lessons, examgen.Lesson{
			ID:             id,
			Title:          l.Title,
			Summary:        l.Summary,
			QuestionBudget: l.QuestionBudget,
		})
		membership = append(membership, examgen.LessonConcepts{LessonID: id, ConceptIDs: l.ConceptIDs})
	}
	return outline, membership, nil
}

func (g *Generator) Questions(ctx context.Context, lesson examgen.Lesson, graph *examgen.EvidenceGraph, c examgen.Chunk, feedback []examgen.RejectedDraft, want int, forceCalc bool) ([]examgen.Question, error) {
	var out struct {
		Questions []examgen.Question `json:"questions"`
	}
	genCtx := WithLabel(ctx, "generate")

	// Phase A: let the model do its arithmetic with a real calculator before it
	// writes anything. See calctool.go for why this beats correcting it after.
	user := examgen.QuestionPrompt(lesson, graph, c, feedback, want, forceCalc)
	if g.UseCalcTool {
		facts, err := g.ComputeFacts(ctx, c, forceCalc)
		if err != nil {
			return nil, fmt.Errorf("calc tool: %w", err)
		}
		user += FactsBlock(facts)
	}

	// Phase B: schema-constrained generation. Tools are deliberately absent —
	// with a format schema set the model will not call them anyway.
	msgs := []Message{
		{Role: "system", Content: examgen.QuestionSystem()},
		{Role: "user", Content: user},
	}
	// A little temperature here: at 0 the model writes the same question from
	// every chunk that looks alike.
	if err := g.c.ChatJSON(genCtx, g.model, msgs, examgen.QuestionSchema(forceCalc), genOptions(8192, 0.3), &out); err != nil {
		return nil, err
	}

	// The schema cannot express "a calculation is optional but must be complete
	// when present", so drop half-filled ones rather than let gate 4 fail on an
	// artefact of the schema.
	for i := range out.Questions {
		dropEmptyCalculation(&out.Questions[i])
	}
	return out.Questions, nil
}

func dropEmptyCalculation(q *examgen.Question) {
	if q.Calculation != nil && q.Calculation.Expression == "" {
		q.Calculation = nil
	}
}
