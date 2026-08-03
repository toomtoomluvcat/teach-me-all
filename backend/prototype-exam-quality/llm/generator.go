package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"protoexam/examgen"
)

// repairResponse tolerates the three shapes hosted models commonly use for a
// one-question correction, even when the supplied schema names questions[].
type repairResponse struct {
	Questions []examgen.Question
}

type distractorReplacement struct {
	Index   int    `json:"index"`
	Content string `json:"content"`
}

type distractorRepairResponse struct {
	Replacements []distractorReplacement `json:"replacements"`
}

func (r *repairResponse) UnmarshalJSON(data []byte) error {
	var envelope struct {
		Questions []examgen.Question `json:"questions"`
		Question  json.RawMessage    `json:"question"`
		Stem      json.RawMessage    `json:"stem"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if len(envelope.Questions) > 0 {
		r.Questions = envelope.Questions
		return nil
	}
	if len(envelope.Question) > 0 && string(envelope.Question) != "null" {
		var q examgen.Question
		if err := json.Unmarshal(envelope.Question, &q); err != nil {
			return err
		}
		r.Questions = []examgen.Question{q}
		return nil
	}
	if len(envelope.Stem) > 0 {
		var q examgen.Question
		if err := json.Unmarshal(data, &q); err != nil {
			return err
		}
		r.Questions = []examgen.Question{q}
	}
	return nil
}

func (r repairResponse) first() (examgen.Question, bool) {
	if len(r.Questions) == 0 {
		return examgen.Question{}, false
	}
	return r.Questions[0], true
}

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

func (g *Generator) Topics(ctx context.Context, c examgen.Chunk) ([]string, error) {
	ctx = WithLabel(ctx, "outline/map")
	var out struct {
		Topics []string `json:"topics"`
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

func (g *Generator) Questions(ctx context.Context, lesson examgen.Lesson, graph *examgen.EvidenceGraph, c examgen.Chunk, want int, forceCalc bool) ([]examgen.Question, error) {
	var out struct {
		Questions []examgen.Question `json:"questions"`
	}
	genCtx := WithLabel(ctx, "generate")

	// Phase A: let the model do its arithmetic with a real calculator before it
	// writes anything. See calctool.go for why this beats correcting it after.
	user := examgen.QuestionPrompt(lesson, graph, c, want, forceCalc)
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

func (g *Generator) Repair(ctx context.Context, q examgen.Question, c examgen.Chunk, failures []examgen.GateResult, forceCalc bool) (examgen.Question, bool, error) {
	ctx = WithLabel(ctx, "repair")
	if verdicts, ok := focusedDistractorAudit(q, failures); ok {
		return g.repairDistractors(ctx, q, c, verdicts)
	}
	var out repairResponse
	msgs := []Message{
		{Role: "system", Content: examgen.RepairSystem()},
		{Role: "user", Content: examgen.RepairPrompt(q, c, failures)},
	}
	// Temperature 0: this is a correction against a stated fact, not a place to
	// explore alternatives.
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.QuestionSchema(forceCalc), genOptions(8192, 0), &out); err != nil {
		return examgen.Question{}, false, err
	}
	fixed, ok := out.first()
	if !ok {
		return examgen.Question{}, false, nil
	}
	if unchanged := mandatoryUnchangedChoices(q, fixed, failures); len(unchanged) > 0 {
		labels := make([]string, len(unchanged))
		for i, index := range unchanged {
			labels[i] = fmt.Sprintf("choice %d", index+1)
		}
		msgs = append(msgs, Message{Role: "user", Content: fmt.Sprintf(
			"CONTRACT FAILURE: %s remained unchanged even though the audit required replacement. Return the whole JSON again and replace that choice with materially different text that is clearly false from the passage.",
			strings.Join(labels, ", "),
		)})
		out = repairResponse{}
		// A deterministic retry repeats the same local optimum even after the
		// contract objection. Small sampling is intentional here: the unchanged
		// wording has already been proven unusable.
		if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.QuestionSchema(forceCalc), genOptions(8192, 0.35), &out); err != nil {
			return examgen.Question{}, false, err
		}
		fixed, ok = out.first()
		if !ok {
			return examgen.Question{}, false, nil
		}
	}
	dropEmptyCalculation(&fixed)
	return fixed, true, nil
}

func focusedDistractorAudit(q examgen.Question, failures []examgen.GateResult) ([]examgen.ChoiceVerdict, bool) {
	correct := q.CorrectIndex()
	if correct < 0 {
		return nil, false
	}
	for _, failure := range failures {
		if failure.Gate != examgen.GateSingleValid || len(failure.ChoiceVerdicts) != len(q.Choices) {
			continue
		}
		for _, verdict := range failure.ChoiceVerdicts {
			if verdict.Index == correct && verdict.Status != examgen.ChoiceSupported {
				return nil, false
			}
		}
		return failure.ChoiceVerdicts, true
	}
	return nil, false
}

func (g *Generator) repairDistractors(ctx context.Context, q examgen.Question, c examgen.Chunk, verdicts []examgen.ChoiceVerdict) (examgen.Question, bool, error) {
	msgs := []Message{
		{Role: "system", Content: examgen.DistractorRepairSystem()},
		{Role: "user", Content: examgen.DistractorRepairPrompt(q, c, verdicts)},
	}
	var out distractorRepairResponse
	err := g.c.ChatJSON(ctx, g.model, msgs, examgen.DistractorRepairSchema(len(q.Choices)), genOptions(8192, 0.3), &out)
	if err != nil {
		return examgen.Question{}, false, err
	}
	fixed, ok := applyDistractorReplacements(q, out.Replacements)
	return fixed, ok, nil
}

func applyDistractorReplacements(q examgen.Question, replacements []distractorReplacement) (examgen.Question, bool) {
	correct := q.CorrectIndex()
	if correct < 0 || len(replacements) != len(q.Choices)-1 {
		return examgen.Question{}, false
	}
	offset := -1
	for _, candidate := range []int{0, 1} {
		seen := make([]bool, len(q.Choices))
		valid := true
		for _, replacement := range replacements {
			index := replacement.Index - candidate
			if index < 0 || index >= len(q.Choices) || index == correct || seen[index] {
				valid = false
				break
			}
			seen[index] = true
		}
		if valid {
			offset = candidate
			break
		}
	}
	if offset < 0 {
		return examgen.Question{}, false
	}
	fixed := q
	fixed.Choices = append([]examgen.Choice(nil), q.Choices...)
	seen := make([]bool, len(q.Choices))
	for _, replacement := range replacements {
		index := replacement.Index - offset
		content := strings.TrimSpace(replacement.Content)
		if index < 0 || index >= len(q.Choices) || index == correct || seen[index] || content == "" || content == strings.TrimSpace(q.Choices[index].Content) {
			return examgen.Question{}, false
		}
		seen[index] = true
		fixed.Choices[index] = examgen.Choice{Content: content}
	}
	for i := range fixed.Choices {
		if i != correct && !seen[i] {
			return examgen.Question{}, false
		}
	}
	return fixed, true
}

func mandatoryUnchangedChoices(before, after examgen.Question, failures []examgen.GateResult) []int {
	correct := before.CorrectIndex()
	seen := map[int]bool{}
	var unchanged []int
	for _, failure := range failures {
		for _, verdict := range failure.ChoiceVerdicts {
			index := verdict.Index
			if index == correct || index < 0 || index >= len(before.Choices) || verdict.Status == examgen.ChoiceUnsupported || seen[index] {
				continue
			}
			seen[index] = true
			if index >= len(after.Choices) || strings.TrimSpace(after.Choices[index].Content) == strings.TrimSpace(before.Choices[index].Content) {
				unchanged = append(unchanged, index)
			}
		}
	}
	return unchanged
}

func dropEmptyCalculation(q *examgen.Question) {
	if q.Calculation != nil && q.Calculation.Expression == "" {
		q.Calculation = nil
	}
}
