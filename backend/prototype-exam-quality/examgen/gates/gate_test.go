package gates

import (
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

func TestGateUnitCheckMatchesPhysicalAnswerUnit(t *testing.T) {
	q := Question{
		Calculation: &Calculation{Expression: "8/2", Expected: 4, Unit: "m/s^2"},
		Choices: []Choice{
			{Content: "4 m/s²", IsCorrect: true},
			{Content: "4 m/s"},
			{Content: "4 N"},
			{Content: "4 kg"},
		},
	}
	got := gateUnitCheck(q)
	if !got.Pass {
		t.Fatalf("matching unit failed: %#v", got)
	}

	q.Choices[0].Content = "4 m/s"
	got = gateUnitCheck(q)
	if got.Pass || !strings.Contains(got.Reason, "missing") {
		t.Fatalf("missing unit passed: %#v", got)
	}
}

func TestGateUnitCheckAcceptsNonPhysicsUnits(t *testing.T) {
	for _, tc := range []struct {
		name string
		unit string
		text string
	}{
		{name: "chemistry", unit: "mol/L", text: "0.25 mol/L"},
		{name: "biology", unit: "%", text: "12.5%"},
		{name: "economics", unit: "USD", text: "125 USD"},
		{name: "temperature", unit: "°C", text: "37 °C"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := Question{
				Calculation: &Calculation{Expression: "5/2", Expected: 2.5, Unit: tc.unit},
				Choices:     []Choice{{Content: tc.text, IsCorrect: true}, {Content: "0"}},
			}
			got := gateUnitCheck(q)
			if !got.Pass {
				t.Fatalf("unit %q failed: %#v", tc.unit, got)
			}
		})
	}
}

func TestGateArithmeticRejectsSymbolicOnlyAnswerForNumericCalculation(t *testing.T) {
	q := Question{
		Skill:               "understanding",
		RequiresCalculation: true,
		Calculation:         &Calculation{Expression: "2-8", Expected: -6},
		Choices:             []Choice{{Content: "1/b^6", IsCorrect: true}, {Content: "b^6"}},
	}
	got := gateArithmetic(q, Arith{})
	if got.Pass {
		t.Fatalf("symbolic answer passed numeric calculation gate: %#v", got)
	}
}

func TestGateArithmeticAllowsNormalThreeDecimalRounding(t *testing.T) {
	q := Question{
		Skill:               "application",
		RequiresCalculation: true,
		Calculation:         &Calculation{Expression: "(20^2*0.866025)/9.8", Expected: 35.348},
		Choices:             []Choice{{Content: "35.348", IsCorrect: true}, {Content: "35.346"}},
	}
	if got := gateArithmetic(q, Arith{}); !got.Pass {
		t.Fatalf("normal rounded numeric answer was rejected: %#v", got)
	}
	// expected tolerates the same ordinary rounding as the choice text. A
	// wrong expected alone must not fail a question whose keyed choice is the
	// correct rounded value (the gate's job is to catch a wrong answer key,
	// which the choice check below does).
	q.Calculation.Expected = 35.346
	if got := gateArithmetic(q, Arith{}); !got.Pass {
		t.Fatalf("expected-only mismatch rejected a question with a correct keyed choice: %#v", got)
	}
	// But a correct expected with a wrong keyed choice must still fail.
	q.Calculation.Expected = 35.348
	q.Choices[0] = Choice{Content: "35.3", IsCorrect: true}
	if got := gateArithmetic(q, Arith{}); got.Pass {
		t.Fatalf("wrong keyed choice passed: %#v", got)
	}
}

func TestGateArithmeticAllowsRoundedExpectedBelowOne(t *testing.T) {
	// expected is a rounded human-readable value (the same number the model
	// put in the correct choice), so it must tolerate the same ordinary 1e-3
	// rounding as the choice-text matcher. A tighter check used to reject
	// "0.2648" for 0.26473265 even though the keyed choice passed.
	q := Question{
		Skill:               "application",
		RequiresCalculation: true,
		Calculation:         &Calculation{Expression: "0.09113*23.24/1000*5/2/0.02000", Expected: 0.2648},
		Choices:             []Choice{{Content: "0.2648", IsCorrect: true}, {Content: "0.2650"}},
	}
	if got := gateArithmetic(q, Arith{}); !got.Pass {
		t.Fatalf("rounded expected below one was rejected: %#v", got)
	}
	q.Calculation.Expected = 0.2650
	if got := gateArithmetic(q, Arith{}); got.Pass {
		t.Fatalf("wrong rounded expected passed: %#v", got)
	}
}

func TestGateDemandContractRejectsOverclaimedHardApplication(t *testing.T) {
	q := Question{
		Skill: "application", Difficulty: "hard", Choices: []Choice{
			{Content: "correct", IsCorrect: true}, {Content: "wrong one"}, {Content: "wrong two"}, {Content: "wrong three"},
		},
		Explanation: "Because the rule applies, therefore the answer follows.",
	}
	got := gateDemandContract(q)
	if got.Pass || !strings.Contains(got.Reason, "changed condition") {
		t.Fatalf("overclaimed hard application passed: %#v", got)
	}
}

func TestGateDemandContractScalesAnalysisReasoningStepsByDifficulty(t *testing.T) {
	base := Question{
		Skill:             "analysis",
		Choices:           []Choice{{Content: "correct", IsCorrect: true}, {Content: "wrong one"}, {Content: "wrong two"}, {Content: "wrong three"}},
		ChangedCondition:  "the sled's mass doubles while friction stays fixed",
		DistractorReasons: []string{"forgets to double the mass term", "uses the old net force", "mixes up thrust and friction"},
	}

	easy := base
	easy.Difficulty = "easy"
	easy.ReasoningSteps = []string{"read the first fact from the source", "read the second, distinct fact from the source"}
	if got := gateDemandContract(easy); !got.Pass {
		t.Fatalf("easy analysis with two distinct reasoning_steps should pass: %#v", got)
	}

	hardTooFew := base
	hardTooFew.Difficulty = "hard"
	hardTooFew.ReasoningSteps = []string{"read the first fact from the source", "read the second, distinct fact from the source"}
	if got := gateDemandContract(hardTooFew); got.Pass {
		t.Fatalf("hard analysis with only two reasoning_steps should need a third: %#v", got)
	}

	hardEnough := base
	hardEnough.Difficulty = "hard"
	hardEnough.ReasoningSteps = []string{"read the first fact from the source", "read the second, distinct fact from the source", "combine both to reach the conclusion"}
	if got := gateDemandContract(hardEnough); !got.Pass {
		t.Fatalf("hard analysis with three distinct reasoning_steps should pass: %#v", got)
	}
}

func TestRunCheapGatesIncludesUnitCheck(t *testing.T) {
	q := Question{Calculation: &Calculation{Expression: "2+2", Expected: 4, Unit: "N"}, Choices: []Choice{{Content: "4", IsCorrect: true}}}
	report := RunCheapGates(q, Chunk{}, Arith{})
	for _, result := range report.Results {
		if result.Gate == GateUnit {
			if result.Pass {
				t.Fatal("unit check passed a keyed choice with no unit")
			}
			return
		}
	}
	t.Fatal("unit check result was not recorded")
}
