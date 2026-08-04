package model

import "context"

// QualityGrader is an advisory semantic review of an already QC-accepted set.
// It must never replace deterministic gates: a model grading its own output
// can be wrong, unavailable, or inconsistent.
type QualityGrader interface {
	GradeSet(ctx context.Context, lesson Lesson, chunks []Chunk, questions []Question) (*QualityReport, error)
}

// QualityVerdict records the dimensions that matter for source-grounded exam
// quality. The strings make the report inspectable instead of hiding judgment
// in one opaque score.
type QualityVerdict struct {
	QuestionIndex      int    `json:"question_index"`
	Score              int    `json:"score"`
	SourceDependency   string `json:"source_dependency"`
	Correctness        string `json:"correctness"`
	DistractorQuality  string `json:"distractor_quality"`
	SkillDifficultyFit string `json:"skill_difficulty_fit"`
	Reason             string `json:"reason"`
}

// QualityReport is indexed against the questions passed to QualityGrader. In
// the set path those are the deterministic-QC-accepted questions only.
type QualityReport struct {
	Verdicts   []QualityVerdict `json:"verdicts"`
	TotalScore int              `json:"total_score"`
	MaxScore   int              `json:"max_score"`
}

// Complete reports are the only ones eligible to influence candidate
// selection. Partial semantic output remains useful for diagnostics but is
// not allowed to create a misleading comparison.
func (r *QualityReport) CompleteFor(questionCount int) bool {
	if r == nil || questionCount == 0 || len(r.Verdicts) != questionCount || r.MaxScore != questionCount*4 {
		return false
	}
	seen := make(map[int]bool, questionCount)
	total := 0
	for _, verdict := range r.Verdicts {
		if verdict.QuestionIndex < 0 || verdict.QuestionIndex >= questionCount || seen[verdict.QuestionIndex] || verdict.Score < 0 || verdict.Score > 4 {
			return false
		}
		seen[verdict.QuestionIndex] = true
		total += verdict.Score
	}
	return total == r.TotalScore && r.TotalScore >= 0 && r.TotalScore <= r.MaxScore
}
