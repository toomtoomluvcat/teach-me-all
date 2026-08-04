package examgen

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
