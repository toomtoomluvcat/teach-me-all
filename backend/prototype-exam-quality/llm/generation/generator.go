package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

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

// Generator adapts a provider client to examgen.Generator.
type Generator struct {
	c     ModelClient
	model string
	// UseCalcTool runs the calculator tool loop before generating, so the model
	// never has to do arithmetic in its head.
	UseCalcTool bool

	// factsCache memoises the calculator tool loop. Every candidate set and
	// every bounded repair for one lesson runs it over the same slot chunks
	// with the same flag, so without this a run pays for an identical
	// multi-round tool conversation once per candidate.
	factsMu    sync.Mutex
	factsCache map[string][]Fact
}

func NewGenerator(c ModelClient, model string) *Generator { return &Generator{c: c, model: model} }

// factsKey identifies the arithmetic scope of one generation run: the lesson
// and the evidence packet it was given.
//
// It deliberately ignores the coverage slots. A bounded repair carries only
// the slots that failed, so keying on slot chunks gave every repair a fresh
// key and made it recompute arithmetic the first candidate had already
// verified. The context packet is fixed once per run, before any candidate, so
// it is stable across candidates and repairs and still distinct between two
// runs over the same lesson with different contracts.
//
// Reusing the wider fact set on a repair is safe: the chunks are the same list
// either way, and every returned fact is arithmetic Go evaluated from that
// packet. The repair simply sees the values its predecessor already paid for.
func factsKey(lesson examgen.Lesson, chunks []examgen.Chunk) string {
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	sort.Strings(ids)
	return lesson.ID + "|" + strings.Join(ids, ",")
}

// cachedFacts runs ComputeFacts once per distinct key.
//
// A failed tool loop is deliberately not cached. It is usually a transport
// error rather than a property of the input, and the next candidate may
// succeed where this one did not.
func (g *Generator) cachedFacts(ctx context.Context, key, text string, requiresCalculation bool) []Fact {
	g.factsMu.Lock()
	if facts, ok := g.factsCache[key]; ok {
		g.factsMu.Unlock()
		return facts
	}
	g.factsMu.Unlock()

	facts, err := g.ComputeFacts(ctx, examgen.Chunk{Page: 0, Text: text}, requiresCalculation)
	if err != nil {
		return nil
	}

	g.factsMu.Lock()
	defer g.factsMu.Unlock()
	if g.factsCache == nil {
		g.factsCache = map[string][]Fact{}
	}
	g.factsCache[key] = facts
	return facts
}

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
	msgs := []Message{
		{Role: "system", Content: examgen.EvidenceCompileSystem()},
		{Role: "user", Content: examgen.EvidenceCompilePrompt(graph, batch)},
	}
	opt := genOptions(evidenceCompileNumCtx, 0)
	opt.NumPredict = evidenceCompileMaxOutputTokens

	var out rawEvidenceResponse
	err := g.c.ChatJSON(ctx, g.model, msgs, examgen.EvidenceCompileSchema(), opt, &out)
	if err == nil {
		return atomsFromRaw(out), nil
	}

	if len(batch) > 1 {
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

	// A single chunk still failed at the standard budget. Rune-count batching
	// cannot split this any further, so the failure is not that the packet is
	// too big — it is that this one chunk is unusually atom-dense (a page of
	// short numbered problems, a dense equation list) and genuinely needs more
	// output room. The provider-level retry inside ChatJSON resends at the same
	// token budget, which reproduces an output-truncation failure identically;
	// only a wider budget can fix that specific failure shape.
	wideOpt := genOptions(evidenceCompileNumCtxWide, 0)
	wideOpt.NumPredict = evidenceCompileMaxOutputTokensWide
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.EvidenceCompileSchema(), wideOpt, &out); err != nil {
		return nil, fmt.Errorf("chunk %s (widened retry): %w", batch[0].ID, err)
	}
	return atomsFromRaw(out), nil
}

func atomsFromRaw(out rawEvidenceResponse) []examgen.EvidenceAtom {
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
	return atoms
}

const (
	// Evidence compilation is a cold, one-time pass, but a single oversized
	// JSON request can spend the entire ten-minute Ollama timeout thinking. Keep
	// batches small enough for the 4B local model to return promptly.
	evidenceBatchMaxChunks         = 4
	evidenceBatchMaxRunes          = 8_000
	evidenceCompileNumCtx          = 12_288
	evidenceCompileMaxOutputTokens = 4_096
	// evidenceCompileMaxOutputTokensWide is the last-resort budget for a single
	// chunk that still fails at the standard budget. Only used on retry, so it
	// costs nothing on the normal path. evidenceCompileNumCtxWide grows the
	// context window to match — a lone chunk can already carry close to
	// evidenceBatchMaxRunes on its own, and that plus an 8k output budget can
	// exceed the standard 12k context window.
	evidenceCompileMaxOutputTokensWide = 8_192
	evidenceCompileNumCtxWide          = 20_480
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
			user += FactsBlock(g.cachedFacts(ctx, factsKey(lesson, chunks), strings.Join(selected, "\n\n"), requiresCalculation))
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

func dropEmptyCalculation(q *examgen.Question) {
	if q.Calculation != nil && q.Calculation.Expression == "" {
		q.Calculation = nil
	}
}
