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
