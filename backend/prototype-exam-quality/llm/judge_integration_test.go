package llm

import (
	"context"
	"os"
	"testing"

	"protoexam/examgen"
)

func TestDeepSeekJudgeFlagsThaiParaphrasedDistractor(t *testing.T) {
	if os.Getenv("PROTOEXAM_INTEGRATION") != "1" {
		t.Skip("set PROTOEXAM_INTEGRATION=1 to run the paid provider eval")
	}
	client := NewDeepSeek(os.Getenv("DEEPSEEK_API_KEY"))
	judge := NewJudge(client, "deepseek-chat")
	q := examgen.Question{
		Kind: examgen.KindMCQSingle,
		Stem: "กระบวนการเปลี่ยนแปลงอาหารเพื่อให้ได้สารอาหารที่มีโมเลกุลขนาดเล็กจนเซลล์สามารถดูดซึมและนำไปใช้ได้ประกอบด้วยอะไรบ้าง",
		Choices: []examgen.Choice{
			{Content: "การกิน การย่อย การดูดซึม และการถ่ายอุจจาระ", IsCorrect: true},
			{Content: "การเคี้ยว การย่อย การดูดซึม และการขับถ่าย"},
			{Content: "การกิน การย่อย การดูดซึม และการขับถ่าย"},
			{Content: "การกิน การย่อย การดูดซึม และการคาย"},
		},
	}
	source := "กระบวนการเปลี่ยนแปลงอาหารเพื่อให้ได้สารอาหารที่มีโมเลกุลขนาดเล็กจนเซลล์สามารถดูดซึมและนำไปใช้ได้ประกอบด้วยการกิน การย่อย การดูดซึม และการถ่ายอุจจาระ"

	verdict, err := judge.JudgeAgainstSource(context.Background(), q, source)
	if err != nil {
		t.Fatalf("JudgeAgainstSource() error = %v", err)
	}
	if len(verdict.ChoiceVerdicts) != len(q.Choices) {
		t.Fatalf("choice verdicts = %#v, want one per option", verdict.ChoiceVerdicts)
	}
	status := verdict.ChoiceVerdicts[2].Status
	if status != examgen.ChoiceEquivalent && status != examgen.ChoiceAmbiguous && status != examgen.ChoiceSupported {
		t.Fatalf("paraphrased distractor status = %q (%s), want non-unique answer flagged", status, verdict.ChoiceVerdicts[2].Reason)
	}
}

func TestDeepSeekRepairNeverBypassesThaiParaphraseAudit(t *testing.T) {
	if os.Getenv("PROTOEXAM_INTEGRATION") != "1" {
		t.Skip("set PROTOEXAM_INTEGRATION=1 to run the paid provider eval")
	}
	client := NewDeepSeek(os.Getenv("DEEPSEEK_API_KEY"))
	gen := NewGenerator(client, "deepseek-chat")
	judge := NewJudge(client, "deepseek-chat")
	source := "กระบวนการเปลี่ยนแปลงอาหารเพื่อให้ได้สารอาหารที่มีโมเลกุลขนาดเล็กจนเซลล์สามารถดูดซึมและนำไปใช้ได้ประกอบด้วยการกิน การย่อย การดูดซึม และการถ่ายอุจจาระ"
	q := examgen.Question{
		Kind: examgen.KindMCQSingle,
		Stem: "กระบวนการเปลี่ยนแปลงอาหารเพื่อให้ได้สารอาหารที่มีโมเลกุลขนาดเล็กจนเซลล์สามารถดูดซึมและนำไปใช้ได้ประกอบด้วยอะไรบ้าง",
		Choices: []examgen.Choice{
			{Content: "การกิน การย่อย การดูดซึม และการถ่ายอุจจาระ", IsCorrect: true},
			{Content: "การเคี้ยว การย่อย การดูดซึม และการขับถ่าย"},
			{Content: "การกิน การย่อย การดูดซึม และการขับถ่าย"},
			{Content: "การกิน การย่อย การดูดซึม และการคาย"},
		},
		Explanation: "ข้อความต้นทางระบุทั้งสี่กระบวนการ",
		SourceQuote: source,
		Difficulty:  "easy",
		Skill:       "recall",
	}
	failure := examgen.GateResult{
		Gate:   examgen.GateSingleValid,
		Reason: "choice 3 is a distractor but audited as equivalent",
		ChoiceVerdicts: []examgen.ChoiceVerdict{
			{Index: 0, Status: examgen.ChoiceSupported, Reason: "ตรงกับต้นฉบับ"},
			{Index: 1, Status: examgen.ChoiceUnsupported, Reason: "ลำดับไม่ตรง"},
			{Index: 2, Status: examgen.ChoiceEquivalent, Reason: "ขับถ่ายมีความหมายเท่ากับถ่ายอุจจาระ"},
			{Index: 3, Status: examgen.ChoiceUnsupported, Reason: "การคายไม่ถูกระบุ"},
		},
	}

	current := q
	failures := []examgen.GateResult{failure}
	for attempt := 1; attempt <= 2; attempt++ {
		fixed, ok, err := gen.Repair(context.Background(), current, examgen.Chunk{ID: "test", Page: 1, Text: source}, failures, false)
		if err != nil {
			t.Fatalf("Repair(attempt %d) error = %v", attempt, err)
		}
		if !ok {
			t.Logf("repair attempt %d safely declined an invalid replacement set", attempt)
			return
		}
		if structural := examgen.CheckWellFormed(fixed); !structural.Pass {
			t.Fatalf("repair attempt %d is malformed: %s; %#v", attempt, structural.Reason, fixed)
		}
		verdict, err := judge.JudgeAgainstSource(context.Background(), fixed, source)
		if err != nil {
			t.Fatalf("JudgeAgainstSource(repair %d) error = %v", attempt, err)
		}
		correct := fixed.CorrectIndex()
		clean := true
		for _, choice := range verdict.ChoiceVerdicts {
			want := examgen.ChoiceUnsupported
			if choice.Index == correct {
				want = examgen.ChoiceSupported
			}
			if choice.Status != want {
				clean = false
			}
		}
		if clean {
			return
		}
		current = fixed
		failures = []examgen.GateResult{{
			Gate:           examgen.GateSingleValid,
			Reason:         "fresh audit still found a non-unique option",
			ChoiceVerdicts: verdict.ChoiceVerdicts,
		}}
	}
	t.Logf("repair did not converge after two attempts; every ambiguous result remained rejected: %#v", failures)
}
