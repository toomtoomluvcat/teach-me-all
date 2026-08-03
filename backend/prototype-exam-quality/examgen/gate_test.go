package examgen

import (
	"context"
	"strings"
	"testing"
)

type semanticChoiceJudge struct {
	sourced SourcedVerdict
}

func (semanticChoiceJudge) JudgeBlind(context.Context, Question) (BlindVerdict, error) {
	return BlindVerdict{Interpretable: true}, nil
}

func (j semanticChoiceJudge) JudgeAgainstSource(context.Context, Question, string) (SourcedVerdict, error) {
	return j.sourced, nil
}

func TestRunGatesRejectsDistractorEquivalentToCorrectChoice(t *testing.T) {
	quote := "ประกอบด้วยการกิน การย่อย การดูดซึม และการถ่ายอุจจาระ"
	q := Question{
		Kind:        KindMCQSingle,
		Stem:        "กระบวนการเปลี่ยนแปลงอาหารประกอบด้วยอะไรบ้าง?",
		SourceQuote: quote,
		Choices: []Choice{
			{Content: "การกิน การย่อย การดูดซึม และการถ่ายอุจจาระ", IsCorrect: true},
			{Content: "การเคี้ยว การย่อย การดูดซึม และการขับถ่าย"},
			{Content: "การกิน การย่อย การดูดซึม และการขับถ่าย"},
			{Content: "การกิน การย่อย การดูดซึม และการคาย"},
		},
	}
	judge := semanticChoiceJudge{sourced: SourcedVerdict{
		BestIndex: 0,
		ChoiceVerdicts: []ChoiceVerdict{
			{Index: 0, Status: ChoiceSupported, Reason: "ตรงกับข้อความต้นทาง"},
			{Index: 1, Status: ChoiceUnsupported, Reason: "เปลี่ยนขั้นตอนการกินเป็นการเคี้ยว"},
			{Index: 2, Status: ChoiceEquivalent, Reason: "ขับถ่ายตีความเป็นการถ่ายอุจจาระได้"},
			{Index: 3, Status: ChoiceUnsupported, Reason: "การคายไม่ใช่ขั้นตอนที่ระบุ"},
		},
	}}

	report, err := RunGates(context.Background(), q, Chunk{ID: "p31-c43", Page: 31, Text: quote}, judge, Arith{})
	if err != nil {
		t.Fatalf("RunGates() error = %v", err)
	}
	var single GateResult
	for _, result := range report.Results {
		if result.Gate == GateSingleValid {
			single = result
			break
		}
	}
	if single.Pass {
		t.Fatalf("single_defensible passed; want equivalent distractor rejected: %s", single.Reason)
	}
	if !strings.Contains(single.Reason, "choice 3") || !strings.Contains(single.Reason, "ขับถ่าย") {
		t.Fatalf("reason = %q, want per-choice semantic evidence", single.Reason)
	}
	if len(single.ChoiceVerdicts) != 4 || single.ChoiceVerdicts[2].Status != ChoiceEquivalent {
		t.Fatalf("persisted choice audit = %#v, want all four option verdicts", single.ChoiceVerdicts)
	}
}
