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

func flawedWorkQuestion() Question {
	return Question{
		Kind:                KindMCQSingle,
		Stem:                "A student computes the acceleration of a 5.0 kg crate under a 12 N net force as 12 x 5.0 = 60 m/s^2. What is wrong with this work?",
		Skill:               "error-finding",
		RequiresCalculation: true,
		Calculation:         &Calculation{Expression: "12/5.0", Expected: 2.4},
		FlawedExpression:    "12*5.0",
		Choices: []Choice{
			{Content: "force was multiplied by mass instead of divided", IsCorrect: true},
			{Content: "the net force should have been halved"},
			{Content: "the mass was converted to grams"},
			{Content: "the units of acceleration were inverted"},
		},
	}
}

func TestGateFlawedWorkAcceptsAWrongResultShownInTheStem(t *testing.T) {
	if res := gateFlawedWork(flawedWorkQuestion(), Arith{}); !res.Pass {
		t.Fatalf("expected pass, got %q", res.Reason)
	}
}

func TestGateFlawedWorkPassesWhenNothingDeclared(t *testing.T) {
	if res := gateFlawedWork(numericQuestion(), Arith{}); !res.Pass {
		t.Fatalf("undeclared flawed work must pass trivially, got %q", res.Reason)
	}
}

// The item only works if the student can see the number they are asked to
// check. A stem that never prints the wrong result is a recall question.
func TestGateFlawedWorkRejectsResultMissingFromStem(t *testing.T) {
	q := flawedWorkQuestion()
	q.Stem = "A student computes the acceleration of a 5.0 kg crate under a 12 N net force incorrectly. What is wrong with this work?"
	if res := gateFlawedWork(q, Arith{}); res.Pass {
		t.Fatal("a flawed result the student never sees cannot be found")
	}
}

func TestGateFlawedWorkRejectsWorkThatIsNotActuallyWrong(t *testing.T) {
	q := flawedWorkQuestion()
	q.Stem = "A student computes the acceleration of a 5.0 kg crate under a 12 N net force as 24/10 = 2.4 m/s^2. What is wrong with this work?"
	q.FlawedExpression = "24/10"
	if res := gateFlawedWork(q, Arith{}); res.Pass {
		t.Fatal("work that reaches the correct value contains no mistake to find")
	}
}

func TestGateFlawedWorkRejectsUnevaluableWork(t *testing.T) {
	q := flawedWorkQuestion()
	q.FlawedExpression = "12 times m"
	if res := gateFlawedWork(q, Arith{}); res.Pass {
		t.Fatal("flawed work that does not evaluate cannot be proved wrong")
	}
}

// A wrong option printed to two significant figures is still that error path's
// number. Holding distractors to the answer key's precision rejected sound
// items for rounding, which is not what the field is checking for.
func TestGateDistractorPathAcceptsOrdinaryRounding(t *testing.T) {
	q := numericQuestion()
	q.Choices[2].Content = "0.42 m/s^2" // 5.0/12 = 0.41667
	if res := gateDistractorPath(q, Arith{}); !res.Pass {
		t.Fatalf("two-significant-figure rounding was rejected: %q", res.Reason)
	}
}

func TestGateDistractorPathStillRejectsAnInventedNumber(t *testing.T) {
	q := numericQuestion()
	q.Choices[2].Content = "0.55 m/s^2" // nowhere near 5.0/12
	if res := gateDistractorPath(q, Arith{}); res.Pass {
		t.Fatal("a number the error path does not produce must still fail")
	}
}

// The writer fills flawed_expression with the correct arithmetic in about half
// of all error-finding drafts and does not stop when told to. The work is
// already on the page — the stem has to show it — so it is read from there.
func TestRepairErrorFindingReadsTheStemWhenTheWriterCopiedTheCorrectExpression(t *testing.T) {
	q := flawedWorkQuestion()
	q.FlawedExpression = "12/5.0" // the correct arithmetic, copied
	q = RepairErrorFinding(q, Arith{})
	if q.FlawedExpression != "12 * 5.0" {
		t.Fatalf("flawed work = %q, want the stem's own equation", q.FlawedExpression)
	}
	if res := gateFlawedWork(q, Arith{}); !res.Pass {
		t.Fatalf("recovered work did not survive the gate: %q", res.Reason)
	}
}

func TestRepairErrorFindingFillsAnEmptyField(t *testing.T) {
	q := flawedWorkQuestion()
	q.FlawedExpression = ""
	q = RepairErrorFinding(q, Arith{})
	if q.FlawedExpression == "" {
		t.Fatal("an equation present in the stem should have been recovered")
	}
}

// A writer that got it right is left alone.
func TestRepairErrorFindingLeavesGoodDraftsUntouched(t *testing.T) {
	q := flawedWorkQuestion()
	if got := RepairErrorFinding(q, Arith{}).FlawedExpression; got != "12*5.0" {
		t.Fatalf("a correct declaration was rewritten to %q", got)
	}
}

// Nothing is invented. A stem that never shows its work still fails, because
// the student had nothing to check either.
func TestRepairErrorFindingInventsNothing(t *testing.T) {
	q := flawedWorkQuestion()
	q.Stem = "A student computes the acceleration of a 5.0 kg crate under a 12 N net force and gets it wrong. What is the mistake?"
	q.FlawedExpression = "12/5.0"
	q = RepairErrorFinding(q, Arith{})
	if res := gateFlawedWork(q, Arith{}); res.Pass {
		t.Fatal("a stem with no displayed work must still fail")
	}
}

// An equation whose own two sides disagree is a typo in the stem, not the
// mistake the question is about.
func TestRepairErrorFindingIgnoresSelfInconsistentStemArithmetic(t *testing.T) {
	q := flawedWorkQuestion()
	q.Stem = "A student writes 12 * 5.0 = 47 m/s^2 for a 5.0 kg crate under a 12 N net force. What is wrong?"
	q.FlawedExpression = "12/5.0"
	q = RepairErrorFinding(q, Arith{})
	if q.FlawedExpression != "12/5.0" {
		t.Fatalf("recovered %q from an equation that does not hold", q.FlawedExpression)
	}
}

// Leaves non-error-finding questions entirely alone.
func TestRepairErrorFindingSkipsOrdinaryQuestions(t *testing.T) {
	q := numericQuestion()
	if got := RepairErrorFinding(q, Arith{}).FlawedExpression; got != "" {
		t.Fatalf("a plain calculation item grew flawed work %q", got)
	}
}
