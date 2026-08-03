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
