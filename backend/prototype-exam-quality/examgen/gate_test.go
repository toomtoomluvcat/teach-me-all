package examgen

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGateQuoteAllowsShortExactNamedFact(t *testing.T) {
	q := Question{SourceQuote: "ไฮดรา พลานาเรีย"}
	report := RunCheapGates(q, Chunk{ID: "p16-c22", Page: 16, Text: q.SourceQuote}, nil)
	for _, result := range report.Results {
		if result.Gate == GateQuote {
			if !result.Pass {
				t.Fatalf("short exact named fact failed quote QC: %s", result.Reason)
			}
			return
		}
	}
	t.Fatal("quote QC result was not recorded")
}

func TestGateQuoteStillRejectsAccidentalSingleToken(t *testing.T) {
	q := Question{SourceQuote: "ไฮดรา"}
	report := RunCheapGates(q, Chunk{ID: "p16-c22", Page: 16, Text: q.SourceQuote}, nil)
	for _, result := range report.Results {
		if result.Gate == GateQuote {
			if result.Pass {
				t.Fatal("single-token quote passed quote QC")
			}
			return
		}
	}
	t.Fatal("quote QC result was not recorded")
}

func TestGateSourceRoleRejectsPrelearningCheck(t *testing.T) {
	quote := "น้ำดีสร้างจากถุงน้ำดีแล้วส่งไปที่ลำไส้เล็กช่วยให้ลิพิดแตกตัว"
	q := Question{SourceQuote: quote}
	report := RunCheapGates(q, Chunk{
		ID:         "p19-c1",
		Page:       19,
		Text:       quote,
		SourceRole: SourceRolePrelearningCheck,
	}, nil)
	for _, result := range report.Results {
		if result.Gate == GateSourceRole {
			if result.Pass || !strings.Contains(result.Reason, "pre-learning check") {
				t.Fatalf("pre-learning check was not rejected: %#v", result)
			}
			return
		}
	}
	t.Fatal("source-role QC result was not recorded")
}

func TestChunkPagesLabelsPrelearningCheckByHeading(t *testing.T) {
	chunks := ChunkPages([]Page{{
		Number: 19,
		Text:   "เฉลยตรวจสอบความรู้ก่อนเรียน\n\n1. ข้อความตามความเข้าใจของนักเรียน",
	}}, ChunkOptions{TargetRunes: 200, OverlapRunes: 0})
	if len(chunks) != 1 {
		t.Fatalf("ChunkPages() returned %d chunks, want 1", len(chunks))
	}
	if chunks[0].SourceRole != SourceRolePrelearningCheck {
		t.Fatalf("SourceRole = %q, want %q", chunks[0].SourceRole, SourceRolePrelearningCheck)
	}
}

type semanticChoiceJudge struct {
	sourced SourcedVerdict
}

func TestGateSourceSpecificRequiresVerifiedPassageFact(t *testing.T) {
	q := Question{Choices: []Choice{
		{Content: "a"},
		{Content: "b", IsCorrect: true},
		{Content: "c"},
		{Content: "d"},
	}}
	chunk := Chunk{ID: "p1-c1", Page: 1, Text: "เมื่อหายใจออกปกติจะมีปริมาตรอากาศที่ตกค้างในปอดเป็น 2,400 mL"}

	cases := []struct {
		name    string
		verdict SourcedVerdict
		pass    bool
	}{
		{
			name: "specific numeric fact passes",
			verdict: SourcedVerdict{
				SourceDependency: SourceDependencySpecific,
				DependencyKind:   DependencyNumber,
				Evidence:         []string{"ปริมาตรอากาศที่ตกค้างในปอดเป็น 2,400 mL"},
				Counterfactual:   true,
			},
			pass: true,
		},
		{
			name: "specific provider object may omit a separate kind",
			verdict: SourcedVerdict{
				SourceDependency: SourceDependencySpecific,
				Evidence:         []string{"ปริมาตรอากาศที่ตกค้างในปอดเป็น 2,400 mL"},
				Counterfactual:   true,
			},
			pass: true,
		},
		{
			name: "generic fact fails",
			verdict: SourcedVerdict{
				SourceDependency: SourceDependencyGeneric,
				DependencyKind:   DependencyNone,
				Counterfactual:   false,
			},
			pass: false,
		},
		{
			name: "specific evidence does not need a redundant counterfactual field",
			verdict: SourcedVerdict{
				SourceDependency: SourceDependencySpecific,
				DependencyKind:   DependencyNumber,
				Evidence:         []string{"2,400 mL"},
				Counterfactual:   false,
			},
			pass: true,
		},
		{
			name: "fabricated evidence fails",
			verdict: SourcedVerdict{
				SourceDependency: SourceDependencySpecific,
				DependencyKind:   DependencyNumber,
				Evidence:         []string{"ปริมาตรอากาศ 9,999 mL"},
				Counterfactual:   true,
			},
			pass: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gateSourceSpecific(q, chunk, tc.verdict)
			if got.Pass != tc.pass {
				t.Fatalf("gateSourceSpecific() pass = %v, want %v (%s)", got.Pass, tc.pass, got.Reason)
			}
			if got.Gate != GateSourceSpecific {
				t.Fatalf("gate = %q, want %q", got.Gate, GateSourceSpecific)
			}
		})
	}

	missing := gateSourceSpecific(q, chunk, SourcedVerdict{SourceDependency: SourceDependencySpecific, DependencyKind: DependencyNumber, Counterfactual: true})
	if missing.Pass || !strings.Contains(missing.Reason, "NOT JUDGED") {
		t.Fatalf("missing evidence was not failed closed: %#v", missing)
	}
}

func TestGateInterpretableNoLongerJudgesGuessability(t *testing.T) {
	// The two gates read the same verdict and must stay separate: one asks
	// whether the question is clear, the other whether the passage was needed.
	q := Question{Choices: []Choice{{Content: "a", IsCorrect: true}, {Content: "b"}}}
	got := gateInterpretable(q, BlindVerdict{Interpretable: true})
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

type partialSourceJudge struct{}

func (partialSourceJudge) JudgeBlind(context.Context, Question) (BlindVerdict, error) {
	return BlindVerdict{Interpretable: true}, nil
}

func (partialSourceJudge) JudgeAgainstSource(context.Context, Question, string) (SourcedVerdict, error) {
	return SourcedVerdict{
		SourceDependency: SourceDependencySpecific,
		Evidence:         []string{"ปริมาตรอากาศที่ตกค้างในปอดเป็น 2,400 mL"},
		Counterfactual:   true,
	}, errors.New("choice audit incomplete")
}

type countingSourceJudge struct {
	calls int
}

func (*countingSourceJudge) JudgeBlind(context.Context, Question) (BlindVerdict, error) {
	return BlindVerdict{Interpretable: true}, nil
}

func (j *countingSourceJudge) JudgeAgainstSource(context.Context, Question, string) (SourcedVerdict, error) {
	j.calls++
	return passingSourceVerdict(), nil
}

func TestRunGatesIsDeterministicQCOnly(t *testing.T) {
	quote := testQuote()
	q := Question{
		Kind:        KindMCQSingle,
		Stem:        "Which process is described by the passage?",
		SourceQuote: quote,
		Choices: []Choice{
			{Content: "It increases the measured value", IsCorrect: true},
			{Content: "It decreases the measured value"},
			{Content: "It keeps the measured value stable"},
			{Content: "It removes the measured value"},
		},
	}
	judge := &countingSourceJudge{}
	report, err := RunGates(context.Background(), q, Chunk{ID: "p1-c1", Page: 1, Text: quote}, judge, Arith{})
	if err != nil {
		t.Fatalf("RunGates() error = %v", err)
	}
	if judge.calls != 0 {
		t.Fatalf("source judge calls = %d, want zero in QC-only mode", judge.calls)
	}
	for _, result := range report.Results {
		if result.Gate == GateSourceSpecific || result.Gate == GateBlindAnswer || result.Gate == GateSingleValid {
			t.Fatalf("non-QC gate ran: %#v", result)
		}
	}
	if !report.Passed() {
		t.Fatalf("valid question failed QC: %#v", report.Failures())
	}
}

func TestAddJudgeGatesDoesNotInvokeModelJudge(t *testing.T) {
	quote := "เมื่อหายใจออกปกติจะมีปริมาตรอากาศที่ตกค้างในปอดเป็น 2,400 mL"
	q := Question{
		Kind:        KindMCQSingle,
		Stem:        "ปริมาตรอากาศที่ตกค้างในปอดมีเท่าใด?",
		SourceQuote: quote,
		Choices: []Choice{
			{Content: "2,400 mL", IsCorrect: true},
			{Content: "1,200 mL"},
			{Content: "3,600 mL"},
			{Content: "4,800 mL"},
		},
	}
	report := RunCheapGates(q, Chunk{ID: "p1-c1", Page: 1, Text: quote}, Arith{})
	if failures := report.Failures(); len(failures) != 0 {
		t.Fatalf("cheap gates rejected test question: %#v", failures)
	}
	if err := AddJudgeGates(context.Background(), report, q, Chunk{ID: "p1-c1", Page: 1, Text: quote}, partialSourceJudge{}); err != nil {
		t.Fatalf("AddJudgeGates() error = %v", err)
	}
	var source GateResult
	for _, result := range report.Results {
		if result.Gate == GateSourceSpecific {
			source = result
		}
		if result.Gate == GateBlindAnswer || result.Gate == GateSingleValid {
			t.Fatalf("deferred gate %q still ran: %#v", result.Gate, result)
		}
	}
	if source.Gate != "" {
		t.Fatalf("source gate = %#v, want no model-backed gate in QC mode", source)
	}
}

func TestRunGatesCorePathUsesOnlyDeterministicQC(t *testing.T) {
	quote := "เมื่อหายใจออกปกติจะมีปริมาตรอากาศที่ตกค้างในปอดเป็น 2,400 mL"
	q := Question{
		Kind:        KindMCQSingle,
		Stem:        "ปริมาตรอากาศที่ตกค้างในปอดมีเท่าใด?",
		SourceQuote: quote,
		Choices: []Choice{
			{Content: "2,400 mL", IsCorrect: true},
			{Content: "1,200 mL"},
			{Content: "3,600 mL"},
			{Content: "4,800 mL"},
		},
	}
	report, err := RunGates(context.Background(), q, Chunk{ID: "p1-c1", Page: 1, Text: quote}, partialSourceJudge{}, Arith{})
	if err != nil {
		t.Fatalf("RunGates() error = %v", err)
	}
	for _, result := range report.Results {
		if result.Gate == GateBlindAnswer || result.Gate == GateSingleValid {
			t.Fatalf("deferred gate %q still ran: %#v", result.Gate, result)
		}
	}
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

	single := gateSingleDefensible(q, judge.sourced)
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
