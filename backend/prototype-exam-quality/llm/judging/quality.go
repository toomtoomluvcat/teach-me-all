package judging

import (
	"context"
	"fmt"
	"strings"

	"protoexam/examgen"
)

// QualityGrader performs one compact batch review per candidate set. It is
// deliberately separate from Judge: Judge is the old per-question experiment,
// while this reviewer is an advisory selector signal and never a hard gate.
type QualityGrader struct {
	c     ModelClient
	model string
}

func NewQualityGrader(c ModelClient, model string) *QualityGrader {
	return &QualityGrader{c: c, model: model}
}

func (g *QualityGrader) GradeSet(ctx context.Context, lesson examgen.Lesson, chunks []examgen.Chunk, questions []examgen.Question) (*examgen.QualityReport, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("quality grader received no questions")
	}
	ctx = WithLabel(ctx, "quality/set")
	selected := qualityChunks(chunks, questions)
	var out struct {
		Questions []examgen.QualityVerdict `json:"questions"`
		// DeepSeek's JSON mode does not enforce the supplied schema. Older
		// responses followed the semantic name "verdicts" despite the contract;
		// accept that harmless alias, then validate exactly as usual.
		Verdicts []examgen.QualityVerdict `json:"verdicts"`
	}
	msgs := []Message{
		{Role: "system", Content: examgen.QualitySystem()},
		{Role: "user", Content: examgen.QualityPrompt(lesson, selected, questions)},
	}
	opt := genOptions(32768, 0)
	opt.NumPredict = 2500
	if err := g.c.ChatJSON(ctx, g.model, msgs, examgen.QualitySchema(), opt, &out); err != nil {
		return nil, err
	}
	if len(out.Questions) == 0 {
		out.Questions = out.Verdicts
	}
	return qualityReport(out.Questions, len(questions))
}

func qualityChunks(chunks []examgen.Chunk, questions []examgen.Question) []examgen.Chunk {
	byID := examgen.ChunkByID(chunks)
	seen := map[string]bool{}
	out := make([]examgen.Chunk, 0, len(questions))
	for _, q := range questions {
		id := strings.TrimSpace(q.EvidenceChunkID)
		if id == "" {
			id = strings.TrimSpace(q.ChunkID)
		}
		if id == "" || seen[id] {
			continue
		}
		if chunk, ok := byID[id]; ok {
			out = append(out, chunk)
			seen[id] = true
		}
	}
	if len(out) == 0 {
		return chunks
	}
	return out
}

func qualityReport(verdicts []examgen.QualityVerdict, questionCount int) (*examgen.QualityReport, error) {
	if len(verdicts) != questionCount {
		return nil, fmt.Errorf("quality grader returned %d verdicts for %d questions", len(verdicts), questionCount)
	}
	seen := make(map[int]bool, questionCount)
	total := 0
	for _, verdict := range verdicts {
		if verdict.QuestionIndex < 0 || verdict.QuestionIndex >= questionCount {
			return nil, fmt.Errorf("quality grader returned out-of-range question index %d", verdict.QuestionIndex)
		}
		if seen[verdict.QuestionIndex] {
			return nil, fmt.Errorf("quality grader repeated question index %d", verdict.QuestionIndex)
		}
		if verdict.Score < 0 || verdict.Score > 4 {
			return nil, fmt.Errorf("quality grader returned invalid score %d", verdict.Score)
		}
		seen[verdict.QuestionIndex] = true
		total += verdict.Score
	}
	report := &examgen.QualityReport{Verdicts: verdicts, TotalScore: total, MaxScore: questionCount * 4}
	if !report.CompleteFor(questionCount) {
		return nil, fmt.Errorf("quality grader returned incomplete verdict coverage")
	}
	return report, nil
}
