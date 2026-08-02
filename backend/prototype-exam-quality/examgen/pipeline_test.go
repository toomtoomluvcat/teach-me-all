package examgen

import (
	"context"
	"testing"
)

type batchTestGenerator struct {
	questions []Question
}

func (g batchTestGenerator) Topics(context.Context, Chunk) ([]string, error) {
	return nil, nil
}

func (g batchTestGenerator) Outline(context.Context, []string) (*Outline, []LessonTopics, error) {
	return nil, nil, nil
}

func (g batchTestGenerator) Questions(context.Context, string, Chunk, int, bool) ([]Question, error) {
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

type recordingTestEmbedder struct {
	calls [][]string
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
