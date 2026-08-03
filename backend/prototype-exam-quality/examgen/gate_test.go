package examgen

import (
	"context"
	"strings"
	"testing"
)

type semanticChoiceJudge struct {
	sourced SourcedVerdict
}

func TestGateNeedsSourceFailsOnlyAConfidentCorrectBlindGuess(t *testing.T) {
	q := Question{Choices: []Choice{
		{Content: "a"},
		{Content: "b", IsCorrect: true},
		{Content: "c"},
		{Content: "d"},
	}}

	cases := []struct {
		name    string
		verdict BlindVerdict
		pass    bool
	}{
		{
			// The defect the first NotebookLM comparison measured: the judge never
			// saw the passage and still knew the answer, so the learner gains
			// nothing by reading it.
			name:    "correct and confident fails",
			verdict: BlindVerdict{GuessedIndex: 1, GuessConfidence: "high"},
			pass:    false,
		},
		{
			name:    "confidence is matched case-insensitively",
			verdict: BlindVerdict{GuessedIndex: 1, GuessConfidence: "HIGH"},
			pass:    false,
		},
		{
			// One in four guesses is right by luck. Low or medium confidence is
			// what an honest guess at an unfamiliar specific looks like.
			name:    "correct but unsure passes",
			verdict: BlindVerdict{GuessedIndex: 1, GuessConfidence: "medium"},
			pass:    true,
		},
		{
			name:    "confident and wrong passes",
			verdict: BlindVerdict{GuessedIndex: 3, GuessConfidence: "high"},
			pass:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gateNeedsSource(q, tc.verdict)
			if got.Pass != tc.pass {
				t.Fatalf("gateNeedsSource() pass = %v, want %v (%s)", got.Pass, tc.pass, got.Reason)
			}
			if got.Gate != GateNeedsSource {
				t.Fatalf("gate = %q, want %q", got.Gate, GateNeedsSource)
			}
		})
	}

	// An absent confidence must be visible, not a quiet pass. The first run with
	// this gate reported "the passage is needed" 17 times while deciding nothing,
	// because the provider omitted the field.
	blank := gateNeedsSource(q, BlindVerdict{GuessedIndex: 1, GuessConfidence: ""})
	if !blank.Pass {
		t.Fatalf("a missing confidence failed the question: %s", blank.Reason)
	}
	if !strings.Contains(blank.Reason, "NOT JUDGED") {
		t.Fatalf("a missing confidence was reported as a real verdict: %s", blank.Reason)
	}

	// A question with no single correct choice is already rejected by
	// gateWellFormed. Failing it twice for one defect makes the reported reason
	// harder to read, not more accurate.
	noKey := Question{Choices: []Choice{{Content: "a"}, {Content: "b"}}}
	if got := gateNeedsSource(noKey, BlindVerdict{GuessedIndex: 0, GuessConfidence: "high"}); !got.Pass {
		t.Fatalf("a question with no answer key was failed here as well: %s", got.Reason)
	}
}

func TestGateInterpretableNoLongerJudgesGuessability(t *testing.T) {
	// The two gates read the same verdict and must stay separate: one asks
	// whether the question is clear, the other whether the passage was needed.
	q := Question{Choices: []Choice{{Content: "a", IsCorrect: true}, {Content: "b"}}}
	got := gateInterpretable(q, BlindVerdict{Interpretable: true, GuessedIndex: 0, GuessConfidence: "high"})
	if !got.Pass {
		t.Fatalf("a clear question was failed by the clarity gate: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "may not be testing comprehension") {
		t.Fatalf("the clarity gate still carries the old advisory note: %s", got.Reason)
	}
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
