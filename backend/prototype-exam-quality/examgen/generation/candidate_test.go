package generation

import (
	"context"
	"testing"
)

// countingGrader records how many advisory reviews the selector actually paid
// for and always returns a complete report so the score can use it.
type countingGrader struct {
	calls int
}

func (g *countingGrader) GradeSet(_ context.Context, _ Lesson, _ []Chunk, questions []Question) (*QualityReport, error) {
	g.calls++
	report := &QualityReport{MaxScore: len(questions) * 4}
	for i := range questions {
		report.Verdicts = append(report.Verdicts, QualityVerdict{QuestionIndex: i, Score: 4})
		report.TotalScore += 4
	}
	return report, nil
}

func passedSet(n int) *ExamResult {
	res := &ExamResult{Budget: 3}
	for i := 0; i < n; i++ {
		q := Question{Stem: "Q", EvidenceAtomID: string(rune('A' + i)), EvidenceChunkID: "c1", Skill: "understanding"}
		res.Passed = append(res.Passed, q)
		res.Questions = append(res.Questions, q)
	}
	return res
}

func TestSelectSetCandidateGradesOnlyCandidatesTiedOnAcceptance(t *testing.T) {
	grader := &countingGrader{}
	winner := passedSet(3)
	drafted := []*ExamResult{passedSet(1), winner, passedSet(2)}

	best := selectSetCandidate(context.Background(), Lesson{Title: "L"}, nil, nil, drafted,
		Deps{Quality: grader})

	if best != winner {
		t.Fatalf("selected a set with %d accepted, want the one with 3", len(best.Passed))
	}
	if grader.calls != 1 {
		t.Fatalf("advisory reviews = %d, want 1 — a candidate that accepted fewer slots cannot win", grader.calls)
	}
	if best.Quality == nil {
		t.Fatal("the shipped set lost its advisory review")
	}
}

func TestSelectSetCandidateGradesEveryCandidateTiedAtTheTop(t *testing.T) {
	grader := &countingGrader{}
	drafted := []*ExamResult{passedSet(2), passedSet(1), passedSet(2)}

	selectSetCandidate(context.Background(), Lesson{Title: "L"}, nil, nil, drafted, Deps{Quality: grader})

	if grader.calls != 2 {
		t.Fatalf("advisory reviews = %d, want 2 — semantic score is the tie-break", grader.calls)
	}
}

func TestSetCandidateScoreKeepsAcceptanceAboveSemanticReview(t *testing.T) {
	// A perfectly-graded smaller set must still lose to a larger accepted one.
	graded := passedSet(2)
	graded.Quality = &QualityReport{
		Verdicts:   []QualityVerdict{{QuestionIndex: 0, Score: 4}, {QuestionIndex: 1, Score: 4}},
		TotalScore: 8, MaxScore: 8,
	}
	ungraded := passedSet(3)

	if setCandidateScore(graded, nil) >= setCandidateScore(ungraded, nil) {
		t.Fatalf("semantic review outranked acceptance: graded=%d ungraded=%d",
			setCandidateScore(graded, nil), setCandidateScore(ungraded, nil))
	}
}
