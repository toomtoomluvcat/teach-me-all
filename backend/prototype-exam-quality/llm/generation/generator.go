package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"protoexam/examgen"
)

// stringListish is a tolerant wire decoder for model metadata. The prompt and
// schema ask for arrays, but hosted models sometimes compress a one-item list
// to a string or return a small variable map. These fields are descriptive
// hints only; provenance still goes through NormalizeEvidenceGraph.
type stringListish []string

func (s *stringListish) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*s = nil
		return nil
	}
	var list []string
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(data, &list); err == nil {
			*s = stringListish(list)
			return nil
		}
		var raw []json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		for _, item := range raw {
			var value string
			if err := json.Unmarshal(item, &value); err == nil && strings.TrimSpace(value) != "" {
				list = append(list, strings.TrimSpace(value))
				continue
			}
			list = append(list, strings.TrimSpace(string(item)))
		}
		*s = stringListish(list)
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*s = stringListish(splitListish(one))
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for key, raw := range object {
		var value string
		if err := json.Unmarshal(raw, &value); err == nil && strings.TrimSpace(value) != "" {
			list = append(list, key+": "+strings.TrimSpace(value))
		} else {
			list = append(list, key+": "+strings.TrimSpace(string(raw)))
		}
	}
	*s = stringListish(list)
	return nil
}

func splitListish(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
	if len(parts) == 0 && strings.TrimSpace(value) != "" {
		return []string{strings.TrimSpace(value)}
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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

func (g *Generator) Topics(ctx context.Context, c examgen.Chunk) (examgen.ChunkTopics, error) {
	ctx = WithLabel(ctx, "outline/map")
	var out examgen.ChunkTopics
	msgs := []Message{
		{Role: "system", Content: examgen.TopicSystem()},
		{Role: "user", Content: examgen.TopicPrompt(c)},
	}
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.TopicSchema(), genOptions(4096, 0), &out); err != nil {
		return examgen.ChunkTopics{}, err
	}
	return out, nil
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

// CompileEvidence turns topic-level graph nodes into source-bound atomic
// claims. It is batched by rune count so a large textbook does not become one
// document-sized prompt, while each atom still retains its exact chunk ID.
func (g *Generator) CompileEvidence(ctx context.Context, graph examgen.EvidenceGraph, chunks []examgen.Chunk) (examgen.EvidenceGraph, error) {
	ctx = WithLabel(ctx, "outline/compile")
	var rawAtoms []examgen.EvidenceAtom
	for _, batch := range evidenceBatches(chunks) {
		atoms, err := g.compileEvidenceBatch(ctx, graph, batch)
		if err != nil {
			return examgen.EvidenceGraph{}, err
		}
		rawAtoms = append(rawAtoms, atoms...)
	}
	return examgen.NormalizeEvidenceGraph(graph, chunks, rawAtoms)
}

type rawEvidenceAtom struct {
	SourceChunkID string        `json:"source_chunk_id"`
	ChunkID       string        `json:"chunk_id"`
	EvidenceChunk string        `json:"evidence_chunk_id"`
	ConceptIDs    []string      `json:"concept_ids"`
	Claim         string        `json:"claim"`
	EvidenceQuote string        `json:"evidence_quote"`
	Relation      string        `json:"relation"`
	Conditions    stringListish `json:"conditions"`
	Variables     stringListish `json:"variables"`
	QuestionForms stringListish `json:"question_forms"`
}

type rawEvidenceResponse struct {
	Atoms []rawEvidenceAtom `json:"atoms"`
}

// compileEvidenceBatch retries a malformed/truncated structured response with
// smaller source packets. Most batches stay at the measured 4-chunk/8k shape;
// dense textbook pages that produce too many atoms are split only on failure.
// This keeps the normal token cost unchanged while preventing one oversized
// response from aborting the entire graph compile.
func (g *Generator) compileEvidenceBatch(ctx context.Context, graph examgen.EvidenceGraph, batch []examgen.Chunk) ([]examgen.EvidenceAtom, error) {
	var out rawEvidenceResponse
	msgs := []Message{
		{Role: "system", Content: examgen.EvidenceCompileSystem()},
		{Role: "user", Content: examgen.EvidenceCompilePrompt(graph, batch)},
	}
	opt := genOptions(evidenceCompileNumCtx, 0)
	opt.NumPredict = evidenceCompileMaxOutputTokens
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.EvidenceCompileSchema(), opt, &out); err != nil {
		if len(batch) <= 1 {
			return nil, err
		}
		mid := len(batch) / 2
		left, leftErr := g.compileEvidenceBatch(ctx, graph, batch[:mid])
		if leftErr != nil {
			return nil, leftErr
		}
		right, rightErr := g.compileEvidenceBatch(ctx, graph, batch[mid:])
		if rightErr != nil {
			return nil, rightErr
		}
		return append(left, right...), nil
	}

	atoms := make([]examgen.EvidenceAtom, 0, len(out.Atoms))
	for _, atom := range out.Atoms {
		chunkID := atom.SourceChunkID
		if chunkID == "" {
			chunkID = atom.ChunkID
		}
		if chunkID == "" {
			chunkID = atom.EvidenceChunk
		}
		atoms = append(atoms, examgen.EvidenceAtom{
			ChunkID:       chunkID,
			ConceptIDs:    atom.ConceptIDs,
			Claim:         atom.Claim,
			Quote:         atom.EvidenceQuote,
			Relation:      atom.Relation,
			Conditions:    []string(atom.Conditions),
			Variables:     []string(atom.Variables),
			QuestionForms: []string(atom.QuestionForms),
		})
	}
	return atoms, nil
}

const (
	// Evidence compilation is a cold, one-time pass, but a single oversized
	// JSON request can spend the entire ten-minute Ollama timeout thinking. Keep
	// batches small enough for the 4B local model to return promptly.
	evidenceBatchMaxChunks         = 4
	evidenceBatchMaxRunes          = 8_000
	evidenceCompileNumCtx          = 12_288
	evidenceCompileMaxOutputTokens = 4_096
)

func evidenceBatches(chunks []examgen.Chunk) [][]examgen.Chunk {
	var batches [][]examgen.Chunk
	for start := 0; start < len(chunks); {
		end := start
		runes := 0
		for end < len(chunks) && end-start < evidenceBatchMaxChunks {
			next := examgen.RuneLen(chunks[end].Text) + examgen.RuneLen(chunks[end].ID) + 32
			if end > start && runes+next > evidenceBatchMaxRunes {
				break
			}
			runes += next
			end++
		}
		batches = append(batches, chunks[start:end])
		start = end
	}
	return batches
}

// PlanQuestions creates one lesson-level coverage plan before the pipeline
// starts making individual question calls. It is optional at the interface
// boundary so the lightweight test generators do not need a planning model.
func (g *Generator) PlanQuestions(ctx context.Context, lesson examgen.Lesson, graph *examgen.EvidenceGraph, chunks []examgen.Chunk, budget int) (examgen.QuestionPlan, error) {
	ctx = WithLabel(ctx, "question-plan")
	var out examgen.QuestionPlan
	msgs := []Message{
		{Role: "system", Content: examgen.QuestionPlanSystem()},
		{Role: "user", Content: examgen.QuestionPlanPrompt(lesson, graph, chunks, budget)},
	}
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.QuestionPlanSchema(), genOptions(16384, 0), &out); err != nil {
		return examgen.QuestionPlan{}, err
	}
	return out, nil
}

// QuestionsSet writes the complete set against the graph-derived contract and
// the lesson's bounded two-hop context. This is the path that can keep target variety
// across chunks instead of resetting the writer on every generation call.
func (g *Generator) QuestionsSet(ctx context.Context, lesson examgen.Lesson, graph *examgen.EvidenceGraph, chunks []examgen.Chunk, contract examgen.CoverageContract, feedback []examgen.RejectedDraft, forceCalc bool) ([]examgen.Question, error) {
	ctx = WithLabel(ctx, "generate-set")
	var out struct {
		Questions []examgen.Question `json:"questions"`
	}
	user := examgen.QuestionSetPrompt(lesson, graph, chunks, contract, feedback, forceCalc)
	if g.UseCalcTool {
		var selected []string
		seen := map[string]bool{}
		requiresCalculation := forceCalc
		for _, slot := range contract.Slots {
			if slot.RequiresCalculation {
				requiresCalculation = true
			}
			if !slot.RequiresCalculation && slot.Skill != "application" {
				continue
			}
			for _, id := range slot.SourceChunkIDs {
				if seen[id] {
					continue
				}
				for _, chunk := range chunks {
					if chunk.ID == id {
						selected = append(selected, chunk.Text)
						seen[id] = true
						break
					}
				}
			}
		}
		if len(selected) > 0 {
			facts, err := g.ComputeFacts(ctx, examgen.Chunk{Page: 0, Text: strings.Join(selected, "\n\n")}, requiresCalculation)
			if err == nil {
				user += FactsBlock(facts)
			}
		}
	}
	msgs := []Message{
		{Role: "system", Content: examgen.QuestionSetSystem()},
		{Role: "user", Content: user},
	}
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.QuestionSetSchemaForContract(forceCalc, contract), genOptions(32768, 0.35), &out); err != nil {
		return nil, err
	}
	for i := range out.Questions {
		dropEmptyCalculation(&out.Questions[i])
	}
	return out.Questions, nil
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
		if err == nil {
			user += FactsBlock(facts)
		}
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
