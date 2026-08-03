package examgen

import (
	"context"
	"strings"
	"testing"
)

type batchTestGenerator struct {
	questions []Question
}

func (g batchTestGenerator) Topics(context.Context, Chunk) ([]string, error) {
	return nil, nil
}

func (g batchTestGenerator) Outline(context.Context, EvidenceGraph) (*Outline, []LessonConcepts, error) {
	return nil, nil, nil
}

func (g batchTestGenerator) Questions(context.Context, Lesson, *EvidenceGraph, Chunk, int, bool) ([]Question, error) {
	return append([]Question(nil), g.questions...), nil
}

func (g batchTestGenerator) Repair(context.Context, Question, Chunk, []GateResult, bool) (Question, bool, error) {
	return Question{}, false, nil
}

type passingTestJudge struct{}

func (passingTestJudge) JudgeBlind(context.Context, Question) (BlindVerdict, error) {
	return BlindVerdict{Interpretable: true}, nil
}

func (passingTestJudge) JudgeAgainstSource(context.Context, Question, string) (SourcedVerdict, error) {
	return SourcedVerdict{BestIndex: 0}, nil
}

type semanticRepairGenerator struct {
	repairCalls int
	failures    []GateResult
}

type twoStepRepairGenerator struct {
	semanticRepairGenerator
}

func (g *twoStepRepairGenerator) Repair(ctx context.Context, q Question, c Chunk, failures []GateResult, forceCalc bool) (Question, bool, error) {
	g.repairCalls++
	g.failures = failures
	if g.repairCalls == 1 {
		q.Choices[3].Content = "intake, digestion, circulation, elimination"
		return q, true, nil
	}
	q.Choices[2].Content = "intake, circulation, absorption, elimination"
	return q, true, nil
}

func (*semanticRepairGenerator) Topics(context.Context, Chunk) ([]string, error) { return nil, nil }
func (*semanticRepairGenerator) Outline(context.Context, EvidenceGraph) (*Outline, []LessonConcepts, error) {
	return nil, nil, nil
}
func (*semanticRepairGenerator) Questions(context.Context, Lesson, *EvidenceGraph, Chunk, int, bool) ([]Question, error) {
	return []Question{{
		Kind: KindMCQSingle, Stem: "Which process sequence is supported by the passage?", SourceQuote: testQuote(),
		Choices: []Choice{
			{Content: "intake, digestion, absorption, elimination", IsCorrect: true},
			{Content: "intake, absorption, digestion, elimination"},
			{Content: "eating, digestion, absorption, excretion"},
			{Content: "intake, digestion, elimination, absorption"},
		},
	}}, nil
}
func (g *semanticRepairGenerator) Repair(_ context.Context, q Question, _ Chunk, failures []GateResult, _ bool) (Question, bool, error) {
	g.repairCalls++
	g.failures = failures
	q.Choices[2].Content = "intake, circulation, absorption, elimination"
	return q, true, nil
}

type semanticRepairJudge struct{}

func (semanticRepairJudge) JudgeBlind(context.Context, Question) (BlindVerdict, error) {
	return BlindVerdict{Interpretable: true}, nil
}
func (semanticRepairJudge) JudgeAgainstSource(_ context.Context, q Question, _ string) (SourcedVerdict, error) {
	statuses := []ChoiceStatus{ChoiceSupported, ChoiceUnsupported, ChoiceEquivalent, ChoiceUnsupported}
	if strings.Contains(q.Choices[2].Content, "circulation") {
		statuses[2] = ChoiceUnsupported
	}
	verdicts := make([]ChoiceVerdict, len(statuses))
	for i, status := range statuses {
		verdicts[i] = ChoiceVerdict{Index: i, Status: status, Reason: string(status)}
	}
	return SourcedVerdict{BestIndex: 0, ChoiceVerdicts: verdicts}, nil
}

type recordingTestEmbedder struct {
	calls [][]string
}

type graphTestGenerator struct {
	graph EvidenceGraph
}

func (g *graphTestGenerator) Topics(context.Context, Chunk) ([]string, error) {
	return nil, nil
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

func (*graphTestGenerator) Questions(context.Context, Lesson, *EvidenceGraph, Chunk, int, bool) ([]Question, error) {
	return nil, nil
}

func (*graphTestGenerator) Repair(context.Context, Question, Chunk, []GateResult, bool) (Question, bool, error) {
	return Question{}, false, nil
}

type graphTopicBatcher struct {
	topics [][]string
}

type omittingGraphGenerator struct{}

func (omittingGraphGenerator) Topics(context.Context, Chunk) ([]string, error) { return nil, nil }

func (omittingGraphGenerator) Outline(_ context.Context, _ EvidenceGraph) (*Outline, []LessonConcepts, error) {
	return &Outline{CourseTitle: "Biology", Lessons: []Lesson{
			{ID: "L01", Title: "Digestion", QuestionBudget: 2},
			{ID: "L02", Title: "Respiration", QuestionBudget: 2},
		}}, []LessonConcepts{
			{LessonID: "L01", ConceptIDs: []string{"C001"}},
			{LessonID: "L02", ConceptIDs: []string{"C003"}},
		}, nil
}

func (omittingGraphGenerator) Questions(context.Context, Lesson, *EvidenceGraph, Chunk, int, bool) ([]Question, error) {
	return nil, nil
}

func (omittingGraphGenerator) Repair(context.Context, Question, Chunk, []GateResult, bool) (Question, bool, error) {
	return Question{}, false, nil
}

func (b graphTopicBatcher) BatchTopics(context.Context, []Chunk, Progress) ([][]string, error) {
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
		TopicBatcher: graphTopicBatcher{topics: [][]string{
			{"Digestion", "Absorption"},
			{"Absorption"},
			{"NON_CONTENT"},
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
		t.Fatalf("lesson assignments = %#v, want content assigned and NON_CONTENT skipped", assigned)
	}
}

func TestBuildOutlineRecoversOmittedConceptThroughEvidenceEdge(t *testing.T) {
	chunks := []Chunk{
		{ID: "p1-c0", Page: 1, Text: "digestion and absorption"},
		{ID: "p2-c1", Page: 2, Text: "respiration"},
	}
	outline, assigned, err := BuildOutline(context.Background(), chunks, Deps{
		Gen: omittingGraphGenerator{},
		TopicBatcher: graphTopicBatcher{topics: [][]string{
			{"Digestion", "Absorption"},
			{"Respiration"},
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
		Judge:    passingTestJudge{},
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

func TestGenerateExamRepairsSemanticChoiceFailureOnce(t *testing.T) {
	gen := &semanticRepairGenerator{}
	outline := &Outline{Lessons: []Lesson{{ID: "L01", Title: "Lesson", QuestionBudget: 1, ChunkIDs: []string{"c1"}}}}
	lesson := outline.Lessons[0]
	chunk := Chunk{ID: "c1", Page: 1, Text: testQuote(), LessonID: lesson.ID}

	res, err := GenerateExam(context.Background(), outline, lesson, []Chunk{chunk}, Deps{
		Gen: gen, Judge: semanticRepairJudge{}, Eval: Arith{},
	}, ExamOptions{Budget: 1, PerChunk: 1, MaxChunkVisits: 1, Repair: true})
	if err != nil {
		t.Fatalf("GenerateExam() error = %v", err)
	}
	if gen.repairCalls != 1 || res.RepairAttempts != 1 || res.RepairsAccepted != 1 {
		t.Fatalf("repair calls/attempts/accepted = %d/%d/%d, want 1/1/1", gen.repairCalls, res.RepairAttempts, res.RepairsAccepted)
	}
	if len(res.Passed) != 1 || !strings.Contains(res.Passed[0].Choices[2].Content, "circulation") {
		t.Fatalf("passed = %#v, want repaired question", res.Passed)
	}
	if len(gen.failures) != 1 || len(gen.failures[0].ChoiceVerdicts) != 4 || gen.failures[0].ChoiceVerdicts[2].Status != ChoiceEquivalent {
		t.Fatalf("repair feedback = %#v, want full per-choice audit", gen.failures)
	}
}

func TestGenerateExamUsesSecondSemanticRepairWhenFreshAuditFindsAnotherAmbiguity(t *testing.T) {
	gen := &twoStepRepairGenerator{}
	outline := &Outline{Lessons: []Lesson{{ID: "L01", Title: "Lesson", QuestionBudget: 1, ChunkIDs: []string{"c1"}}}}
	lesson := outline.Lessons[0]
	chunk := Chunk{ID: "c1", Page: 1, Text: testQuote(), LessonID: lesson.ID}

	res, err := GenerateExam(context.Background(), outline, lesson, []Chunk{chunk}, Deps{
		Gen: gen, Judge: semanticRepairJudge{}, Eval: Arith{},
	}, ExamOptions{Budget: 1, PerChunk: 1, MaxChunkVisits: 1, Repair: true, MaxRepairs: 2})
	if err != nil {
		t.Fatalf("GenerateExam() error = %v", err)
	}
	if gen.repairCalls != 2 || res.RepairAttempts != 2 || res.RepairsAccepted != 1 {
		t.Fatalf("repair calls/attempts/accepted = %d/%d/%d, want 2/2/1", gen.repairCalls, res.RepairAttempts, res.RepairsAccepted)
	}
	if len(res.Passed) != 1 {
		t.Fatalf("passed = %#v, want second repair accepted", res.Passed)
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
