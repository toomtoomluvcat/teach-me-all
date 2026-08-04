package judging

import (
	"testing"

	"protoexam/examgen"
)

func TestQualityReportRejectsIncompleteOrInconsistentOutput(t *testing.T) {
	if _, err := qualityReport([]examgen.QualityVerdict{{QuestionIndex: 0, Score: 4}}, 2); err == nil {
		t.Fatal("qualityReport accepted a missing verdict")
	}
	if _, err := qualityReport([]examgen.QualityVerdict{{QuestionIndex: 0, Score: 5}}, 1); err == nil {
		t.Fatal("qualityReport accepted an out-of-range score")
	}
	report, err := qualityReport([]examgen.QualityVerdict{{QuestionIndex: 0, Score: 3}, {QuestionIndex: 1, Score: 2}}, 2)
	if err != nil {
		t.Fatalf("qualityReport() error = %v", err)
	}
	if report.TotalScore != 5 || report.MaxScore != 8 || !report.CompleteFor(2) {
		t.Fatalf("report = %#v", report)
	}
}
