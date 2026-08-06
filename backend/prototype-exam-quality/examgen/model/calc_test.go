package model

import (
	"math"
	"testing"
)

func TestArithSupportsWhitelistedPhysicsFunctions(t *testing.T) {
	tests := []struct {
		expression string
		want       float64
	}{
		{expression: "sin(30*pi/180)", want: 0.5},
		{expression: "sqrt(3^2 + 4^2)", want: 5},
		{expression: "abs(-7.5)", want: 7.5},
		{expression: "ln(exp(2))", want: 2},
	}
	for _, test := range tests {
		got, err := (Arith{}).Eval(test.expression)
		if err != nil {
			t.Fatalf("Eval(%q) error: %v", test.expression, err)
		}
		if math.Abs(got-test.want) > 1e-9 {
			t.Errorf("Eval(%q) = %g, want %g", test.expression, got, test.want)
		}
	}
}

func TestArithRejectsUnknownFunctionsAndIdentifiers(t *testing.T) {
	for _, expression := range []string{"pow(2, 3)", "answer + 1", "sqrt(-1)"} {
		if _, err := (Arith{}).Eval(expression); err == nil {
			t.Errorf("Eval(%q) succeeded; want a safe parser error", expression)
		}
	}
}

func TestChoiceMentionsUnicodeScientificNotation(t *testing.T) {
	if !choiceMentionsNumber("approximately 1.04 × 10⁵ N", 103550) {
		t.Fatal("rounded Unicode scientific notation should match the computed value")
	}
	if choiceMentionsNumber("1.03 × 10⁵ N", 103550) {
		t.Fatal("incorrect rounded scientific notation should not match")
	}
}

func TestChoiceMentionsNumberWords(t *testing.T) {
	if !choiceMentionsNumber("four times greater", 4) {
		t.Fatal("number words should match a computed integer")
	}
	if choiceMentionsNumber("five times greater", 4) {
		t.Fatal("wrong number word should not match")
	}
}

func TestChoiceMentionsNumberRequiresApproxMarkerForCoarseRounding(t *testing.T) {
	if choiceMentionsNumber("1.7 N", 1.67) {
		t.Fatal("1.7 silently drops real precision from 1.67 and should not match without an approximate marker")
	}
	if !choiceMentionsNumber("about 1.7 N", 1.67) {
		t.Fatal("1.7 should match once the choice marks itself approximate")
	}
	if !choiceMentionsNumber("≈1.7 N", 1.67) {
		t.Fatal("1.7 should match once the choice marks itself approximate")
	}
	if !choiceMentionsNumber("1.67 N", 1.67) {
		t.Fatal("the exact value should always match")
	}
}

func TestChoiceMentionsNumberAllowsOrdinaryThreeDecimalRounding(t *testing.T) {
	if !choiceMentionsNumber("35.348", 35.34795918367347) {
		t.Fatal("ordinary three-decimal rounding on a value >= 1 should not need an approximate marker")
	}
}

func TestChoiceMentionsNumberAllowsThreeSigFigRoundingAtAnyMagnitude(t *testing.T) {
	// Titration/percentage-scale answers routinely round to 3 significant
	// figures without an "approximately" marker — this is the real shape that
	// exposed an absolute (not relative) tolerance being wrong at this scale.
	if !choiceMentionsNumber("69.9%", 69.914778) {
		t.Fatal("69.9% should match a computed 69.914778 (3 sig figs, ~0.02% relative)")
	}
	if !choiceMentionsNumber("22.9%", 22.887473) {
		t.Fatal("22.9% should match a computed 22.887473 (3 sig figs, ~0.05% relative)")
	}
	if choiceMentionsNumber("77.1%", 22.887473) {
		t.Fatal("77.1% is a completely different value and must not match")
	}
}

func TestParseExponentHandlesEverySuperscriptDigit(t *testing.T) {
	// ¹²³ live in a different Unicode block than ⁰⁴⁵⁶⁷⁸⁹ — this must not
	// silently produce a garbage value for any of them.
	for _, tc := range []struct {
		s    string
		want int
	}{
		{"⁻³", -3}, {"⁻²", -2}, {"⁻¹", -1}, {"²", 2}, {"³", 3},
		{"¹²", 12}, {"⁻¹³", -13}, {"⁴", 4}, {"⁻⁵", -5},
	} {
		got, ok := parseExponent(tc.s)
		if !ok || got != tc.want {
			t.Fatalf("parseExponent(%q) = %d, %v; want %d, true", tc.s, got, ok, tc.want)
		}
	}
}

func TestChoiceMentionsNumberMatchesNegativeSuperscriptExponent(t *testing.T) {
	if !choiceMentionsNumber("9.6 × 10⁻³ M", 0.009636) {
		t.Fatal("9.6 × 10⁻³ should match 0.009636 — this is a real titration-answer shape")
	}
	if !choiceMentionsNumber("2.9 × 10² g", 290.0) {
		t.Fatal("2.9 × 10² should match 290 — superscript 2 must not be misparsed")
	}
}

func TestChoiceMentionsNumberMatchesWritersOwnRounding(t *testing.T) {
	// A writer may round 0.26473265 to four significant figures as "0.2648"
	// (up) even though Sprintf's %.4f would emit 0.2647. That is still lossless
	// at 1e-3 relative and must match; a genuinely wrong value must not.
	if !choiceMentionsNumber("0.2648", 0.26473265) {
		t.Fatal("writer's four-sig-fig rounding 0.2648 should match 0.26473265")
	}
	if choiceMentionsNumber("0.2650", 0.26473265) {
		t.Fatal("0.2650 is outside 1e-3 relative tolerance and must not match")
	}
}

func TestChoiceMentionsNumberUsesNumericBoundaries(t *testing.T) {
	for _, choice := range []string{"14 N", "40 m/s", "0.4 kg"} {
		if choiceMentionsNumber(choice, 4) {
			t.Fatalf("%q should not match the standalone number 4", choice)
		}
	}
	for _, choice := range []string{"4 N", "4.0 kg", "approximately 4"} {
		if !choiceMentionsNumber(choice, 4) {
			t.Fatalf("%q should match the standalone number 4", choice)
		}
	}
}
