package gates

import "testing"

func numericQuestion() Question {
	return Question{
		Kind:                KindMCQSingle,
		Stem:                "A 5.0 kg crate is pushed with a net force of 12 N. What is its acceleration?",
		RequiresCalculation: true,
		Calculation:         &Calculation{Expression: "12/5.0", Expected: 2.4},
		Choices: []Choice{
			{Content: "2.4 m/s^2", IsCorrect: true},
			{Content: "60 m/s^2", DistractorExpression: "12*5.0"},
			{Content: "0.4167 m/s^2", DistractorExpression: "5.0/12"},
			{Content: "17 m/s^2", DistractorExpression: "12+5.0"},
		},
	}
}

func TestGateDistractorPathAcceptsVerifiedErrorPaths(t *testing.T) {
	res := gateDistractorPath(numericQuestion(), Arith{})
	if !res.Pass {
		t.Fatalf("expected pass, got %q", res.Reason)
	}
}

func TestGateDistractorPathPassesWhenNothingDeclared(t *testing.T) {
	q := numericQuestion()
	for i := range q.Choices {
		q.Choices[i].DistractorExpression = ""
	}
	if res := gateDistractorPath(q, Arith{}); !res.Pass {
		t.Fatalf("undeclared expressions must pass trivially, got %q", res.Reason)
	}
}

// The whole point of the field: an invented number cannot be dressed up with an
// expression that produces something else.
func TestGateDistractorPathRejectsExpressionThatDoesNotProduceTheChoice(t *testing.T) {
	q := numericQuestion()
	q.Choices[1].Content = "99 m/s^2"
	res := gateDistractorPath(q, Arith{})
	if res.Pass {
		t.Fatal("expected rejection when the choice text is not the expression's value")
	}
}

func TestGateDistractorPathRejectsWrongChoiceThatEqualsTheKey(t *testing.T) {
	q := numericQuestion()
	q.Choices[1].Content = "2.4 m/s^2"
	q.Choices[1].DistractorExpression = "24/10"
	res := gateDistractorPath(q, Arith{})
	if res.Pass {
		t.Fatal("a wrong-answer path that lands on the keyed value is not a distractor")
	}
}

func TestGateDistractorPathRejectsExpressionOnTheCorrectChoice(t *testing.T) {
	q := numericQuestion()
	q.Choices[0].DistractorExpression = "12/5.0"
	if res := gateDistractorPath(q, Arith{}); res.Pass {
		t.Fatal("the key must not carry a distractor_expression")
	}
}

func TestGateDistractorPathRejectsUnevaluableExpression(t *testing.T) {
	q := numericQuestion()
	q.Choices[1].DistractorExpression = "12 * m"
	if res := gateDistractorPath(q, Arith{}); res.Pass {
		t.Fatal("an expression that does not evaluate must fail")
	}
}

func decoyQuestion() Question {
	q := numericQuestion()
	q.Stem = "A 5.0 kg crate sits on a bench 0.80 m high and is pushed with a net force of 12 N. What is its acceleration?"
	q.DecoyValues = []string{"0.80"}
	return q
}

func TestGateDecoyAcceptsUnusedStemValue(t *testing.T) {
	if res := gateDecoy(decoyQuestion()); !res.Pass {
		t.Fatalf("expected pass, got %q", res.Reason)
	}
}

func TestGateDecoyPassesWhenNothingDeclared(t *testing.T) {
	if res := gateDecoy(numericQuestion()); !res.Pass {
		t.Fatalf("undeclared decoys must pass trivially, got %q", res.Reason)
	}
}

// A "decoy" the solution consumes is just a given. Accepting it would let the
// noise axis be satisfied without adding any decision for the student.
func TestGateDecoyRejectsValueUsedBySolution(t *testing.T) {
	q := decoyQuestion()
	q.DecoyValues = []string{"5.0"}
	if res := gateDecoy(q); res.Pass {
		t.Fatal("a value the expression uses is a given, not a decoy")
	}
}

func TestGateDecoyRejectsValueMissingFromStem(t *testing.T) {
	q := decoyQuestion()
	q.DecoyValues = []string{"31.7"}
	if res := gateDecoy(q); res.Pass {
		t.Fatal("a decoy the student never sees distracts nobody")
	}
}

func TestGateDecoyRejectsDuplicateDeclaration(t *testing.T) {
	q := decoyQuestion()
	q.DecoyValues = []string{"0.80", "0.80"}
	if res := gateDecoy(q); res.Pass {
		t.Fatal("one distraction declared twice must not count twice")
	}
}

// Non-numeric decoys are presence-checked only: there is no expression for a
// qualitative given to be absent from. This keeps the axis usable on sources
// with no arithmetic at all.
func TestGateDecoyAcceptsQualitativeDecoyPresentInStem(t *testing.T) {
	q := Question{
		Stem:        "A merchant guild in a coastal city with a long rainy season petitions the crown for a monopoly. What does the guild gain?",
		Choices:     []Choice{{Content: "exclusive trading rights", IsCorrect: true}, {Content: "tax exemption"}},
		DecoyValues: []string{"long rainy season"},
	}
	if res := gateDecoy(q); !res.Pass {
		t.Fatalf("expected pass, got %q", res.Reason)
	}
}

func TestGateDecoyRejectsQualitativeDecoyMissingFromStem(t *testing.T) {
	q := Question{
		Stem:        "A merchant guild petitions the crown for a monopoly. What does the guild gain?",
		Choices:     []Choice{{Content: "exclusive trading rights", IsCorrect: true}, {Content: "tax exemption"}},
		DecoyValues: []string{"long rainy season"},
	}
	if res := gateDecoy(q); res.Pass {
		t.Fatal("a qualitative decoy absent from the stem must fail")
	}
}
