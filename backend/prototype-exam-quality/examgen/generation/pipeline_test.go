package generation

import (
	"context"
	"strings"
	"testing"
)

type batchTestGenerator struct {
	questions []Question
}

func (g batchTestGenerator) Topics(context.Context, Chunk) (ChunkTopics, error) {
	return ChunkTopics{}, nil
}

func (g batchTestGenerator) Outline(context.Context, EvidenceGraph) (*Outline, []LessonConcepts, error) {
	return nil, nil, nil
}

func (g batchTestGenerator) Questions(context.Context, Lesson, *EvidenceGraph, Chunk, []RejectedDraft, int, bool) ([]Question, error) {
	return append([]Question(nil), g.questions...), nil
}

type feedbackRecordingGenerator struct {
	calls    int
	feedback [][]RejectedDraft
}

func (*feedbackRecordingGenerator) Topics(context.Context, Chunk) (ChunkTopics, error) {
	return ChunkTopics{}, nil
}
func (*feedbackRecordingGenerator) Outline(context.Context, EvidenceGraph) (*Outline, []LessonConcepts, error) {
	return nil, nil, nil
}
func (g *feedbackRecordingGenerator) Questions(_ context.Context, _ Lesson, _ *EvidenceGraph, _ Chunk, feedback []RejectedDraft, _ int, _ bool) ([]Question, error) {
	g.calls++
	g.feedback = append(g.feedback, append([]RejectedDraft(nil), feedback...))
	q := Question{
		Kind: KindMCQSingle, Stem: "Which process sequence is supported by the passage?", SourceQuote: testQuote(),
		Choices: []Choice{
			{Content: "intake, digestion, absorption, elimination", IsCorrect: true},
			{Content: "intake, absorption, digestion, elimination"},
			{Content: "eating, digestion, absorption, excretion"},
			{Content: "intake, digestion, elimination, absorption"},
		},
	}
	if g.calls > 1 {
		q.Stem = "Which sequence preserves every process named in the source?"
		q.Choices[2].Content = "intake, circulation, absorption, elimination"
	}
	return []Question{q}, nil
}

type recordingTestEmbedder struct {
	calls [][]string
}

type graphTestGenerator struct {
	graph EvidenceGraph
}

type compileRecordingGenerator struct {
	graphTestGenerator
	compiled []Chunk
}

func (g *compileRecordingGenerator) CompileEvidence(_ context.Context, graph EvidenceGraph, chunks []Chunk) (EvidenceGraph, error) {
	g.compiled = append([]Chunk(nil), chunks...)
	return graph, nil
}

func (g *graphTestGenerator) Topics(context.Context, Chunk) (ChunkTopics, error) {
	return ChunkTopics{}, nil
}

func (g *graphTestGenerator) Outline(_ context.Context, graph EvidenceGraph) (*Outline, []LessonConcepts, error) {
	g.graph = graph
	ids := make([]string, len(graph.Concepts))
	for i, concept := range graph.Concepts {
		ids[i] = concept.ID
	}
	return &Outline{CourseTitle: "Biology", Lessons: []Lesson{{ID: "L01", Title: "Digestion", QuestionBudget: 2}}},
		[]LessonConcepts{{LessonID: "L01", ConceptIDs: ids}}, nil
}

func (*graphTestGenerator) Questions(context.Context, Lesson, *EvidenceGraph, Chunk, []RejectedDraft, int, bool) ([]Question, error) {
	return nil, nil
}

// content is the common case in these tests: topics the map step classified as
// subject matter.
func content(titles ...string) ChunkTopics {
	return ChunkTopics{Kind: TopicContent, Topics: titles}
}

type graphTopicBatcher struct {
	topics []ChunkTopics
}

type omittingGraphGenerator struct{}

func (omittingGraphGenerator) Topics(context.Context, Chunk) (ChunkTopics, error) {
	return ChunkTopics{}, nil
}

func (omittingGraphGenerator) Outline(_ context.Context, _ EvidenceGraph) (*Outline, []LessonConcepts, error) {
	return &Outline{CourseTitle: "Biology", Lessons: []Lesson{
			{ID: "L01", Title: "Digestion", QuestionBudget: 2},
			{ID: "L02", Title: "Respiration", QuestionBudget: 2},
		}}, []LessonConcepts{
			{LessonID: "L01", ConceptIDs: []string{"C001"}},
			{LessonID: "L02", ConceptIDs: []string{"C003"}},
		}, nil
}

func (omittingGraphGenerator) Questions(context.Context, Lesson, *EvidenceGraph, Chunk, []RejectedDraft, int, bool) ([]Question, error) {
	return nil, nil
}

func (b graphTopicBatcher) BatchTopics(context.Context, []Chunk, Progress) ([]ChunkTopics, error) {
	return b.topics, nil
}

func TestBuildOutlineCompilesEvidenceGraphWithoutLosingChunkProvenance(t *testing.T) {
	chunks := []Chunk{
		{ID: "p1-c0", Page: 1, Text: "digestion and absorption"},
		{ID: "p2-c1", Page: 2, Text: "absorption"},
		{ID: "p3-c2", Page: 3, Text: "front matter"},
	}
	gen := &graphTestGenerator{}
	outline, assigned, err := BuildOutline(context.Background(), chunks, Deps{
		Gen: gen,
		TopicBatcher: graphTopicBatcher{topics: []ChunkTopics{
			content("Digestion", "Absorption"),
			content("Absorption"),
			{Kind: TopicNonContent},
		}},
	})
	if err != nil {
		t.Fatalf("BuildOutline() error = %v", err)
	}
	if len(gen.graph.Concepts) != 2 {
		t.Fatalf("concepts = %#v, want Digestion and Absorption", gen.graph.Concepts)
	}
	absorption := gen.graph.Concepts[1]
	if absorption.Title != "Absorption" || len(absorption.ChunkIDs) != 2 || absorption.ChunkIDs[0] != "p1-c0" || absorption.ChunkIDs[1] != "p2-c1" {
		t.Fatalf("absorption provenance = %#v", absorption)
	}
	if len(gen.graph.Edges) == 0 || gen.graph.Edges[0].Kind != EdgeCoOccurs {
		t.Fatalf("edges = %#v, want evidenced co-occurrence", gen.graph.Edges)
	}
	if outline.EvidenceGraph == nil || len(outline.Lessons[0].ConceptIDs) != 2 {
		t.Fatalf("outline graph/lesson concepts were not retained: %#v", outline)
	}
	if assigned[0].LessonID != "L01" || assigned[1].LessonID != "L01" || assigned[2].LessonID != "" {
		t.Fatalf("lesson assignments = %#v, want content assigned and page furniture skipped", assigned)
	}
}

func TestBuildOutlineCompilesOnlyContentChunks(t *testing.T) {
	chunks := []Chunk{{ID: "c1", Page: 1, Text: "subject"}, {ID: "c2", Page: 2, Text: "answer key"}}
	gen := &compileRecordingGenerator{}
	_, _, err := BuildOutline(context.Background(), chunks, Deps{
		Gen:          gen,
		CompileGraph: true,
		TopicBatcher: graphTopicBatcher{topics: []ChunkTopics{
			content("Subject"),
			{Kind: TopicApparatus, Topics: []string{"answer key"}},
		}},
	})
	if err != nil {
		t.Fatalf("BuildOutline() error = %v", err)
	}
	if len(gen.compiled) != 1 || gen.compiled[0].ID != "c1" {
		t.Fatalf("compiler received %#v, want only content chunk c1", gen.compiled)
	}
}

func TestBuildOutlineRecoversOmittedConceptThroughEvidenceEdge(t *testing.T) {
	chunks := []Chunk{
		{ID: "p1-c0", Page: 1, Text: "digestion and absorption"},
		{ID: "p2-c1", Page: 2, Text: "respiration"},
	}
	outline, assigned, err := BuildOutline(context.Background(), chunks, Deps{
		Gen: omittingGraphGenerator{},
		TopicBatcher: graphTopicBatcher{topics: []ChunkTopics{
			content("Digestion", "Absorption"),
			content("Respiration"),
		}},
	})
	if err != nil {
		t.Fatalf("BuildOutline() error = %v", err)
	}
	if assigned[0].LessonID != "L01" || assigned[1].LessonID != "L02" {
		t.Fatalf("lesson assignments = %#v", assigned)
	}
	if !containsString(outline.Lessons[0].ConceptIDs, "C002") {
		t.Fatalf("omitted co-occurring concept was not recovered: %#v", outline.Lessons[0])
	}
}

func (e *recordingTestEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, append([]string(nil), texts...))
	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = []float32{float32(i + 1), float32(len(texts) - i)}
	}
	return vecs, nil
}

func TestGenerateExamEmbedsOnlyCheapPassingQuestionsAsOneBatch(t *testing.T) {
	questions := []Question{
		{Stem: "This truncated statement has no question word", Choices: testChoices(), SourceQuote: testQuote()},
		{Stem: "Which concept explains the observed result?", Choices: testChoices(), SourceQuote: testQuote()},
		{Stem: "What changes when the input is increased?", Choices: testChoices(), SourceQuote: testQuote()},
	}
	embedder := &recordingTestEmbedder{}
	outline := &Outline{Lessons: []Lesson{{ID: "L01", Title: "Lesson", Summary: "Summary", QuestionBudget: 2, ChunkIDs: []string{"c1"}}}}
	lesson := outline.Lessons[0]
	chunk := Chunk{ID: "c1", Page: 1, Text: testQuote(), LessonID: lesson.ID}

	res, err := GenerateExam(context.Background(), outline, lesson, []Chunk{chunk}, Deps{
		Gen:      batchTestGenerator{questions: questions},
		Eval:     Arith{},
		Embedder: embedder,
	}, ExamOptions{Budget: 2, PerChunk: 3, MaxChunkVisits: 1})
	if err != nil {
		t.Fatalf("GenerateExam() error = %v", err)
	}
	if len(res.Passed) != 2 {
		t.Fatalf("passed = %d, want 2", len(res.Passed))
	}
	if len(embedder.calls) != 1 {
		t.Fatalf("embedding calls = %d, want 1", len(embedder.calls))
	}
	if got := len(embedder.calls[0]); got != 2 {
		t.Fatalf("texts in embedding batch = %d, want 2", got)
	}
}

func TestRejectedDraftMemoryKeepsOnlyNewestFourAndBoundsText(t *testing.T) {
	var memory []RejectedDraft
	long := strings.Repeat("x", 400)
	for i := 0; i < 6; i++ {
		q := Question{Stem: string(rune('0'+i)) + long, Choices: []Choice{{Content: long}}}
		report := &GateReport{Results: []GateResult{{Gate: GateCoverage, Reason: long}}}
		memory = appendRejectedDraft(memory, q, report)
	}
	if len(memory) != 4 || !strings.HasPrefix(memory[0].Stem, "2") || !strings.HasPrefix(memory[3].Stem, "5") {
		t.Fatalf("bounded memory = %#v", memory)
	}
	if RuneLen(memory[0].Stem) > 240 || RuneLen(memory[0].Choices[0]) > 180 || RuneLen(memory[0].Failures[0].Reason) > 240 {
		t.Fatalf("memory text was not compacted: %#v", memory[0])
	}
}

func TestMissingCoverageContractKeepsOnlyUnacceptedSlots(t *testing.T) {
	contract := CoverageContract{
		Budget:          3,
		ContextChunkIDs: []string{"c1", "c2", "c3"},
		Slots: []CoverageSlot{
			{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}},
			{ID: "S02", AtomID: "A002", SourceChunkIDs: []string{"c2"}},
			{ID: "S03", AtomID: "A003", SourceChunkIDs: []string{"c3"}},
		},
	}
	candidate := &ExamResult{Passed: []Question{{CoverageSlotID: "S02"}}}
	retry, ok := missingCoverageContract(contract, candidate)
	if !ok || len(retry.Slots) != 2 || retry.Slots[0].ID != "S01" || retry.Slots[1].ID != "S03" {
		t.Fatalf("retry contract = %#v, ok=%v", retry, ok)
	}
	if len(contract.Slots) != 3 {
		t.Fatalf("original contract was mutated: %#v", contract)
	}
}

func TestRejectedDraftsOnlyReturnsFailedQuestions(t *testing.T) {
	passed := Question{Stem: "passed"}
	passed.Report = &GateReport{Results: []GateResult{{Gate: GateWellFormed, Pass: true}}}
	failed := Question{Stem: "failed"}
	failed.Report = &GateReport{Results: []GateResult{{Gate: GateQuote, Reason: "bad quote"}}}
	feedback := rejectedDrafts([]Question{passed, failed})
	if len(feedback) != 1 || feedback[0].Stem != "failed" {
		t.Fatalf("feedback = %#v, want only failed question", feedback)
	}
}

func testQuote() string {
	return "The measured value increases when the input changes, according to the experiment."
}

func testChoices() []Choice {
	return []Choice{
		{Content: "It increases the measured value", IsCorrect: true},
		{Content: "It decreases the measured value"},
		{Content: "It keeps the measured value stable"},
		{Content: "It removes the measured value"},
	}
}
