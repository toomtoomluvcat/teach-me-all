package gates

import (
	"fmt"
	"strconv"
	"strings"

	"protoexam/examgen/model"
)

// Two deterministic checks for the parts of item quality that prose could only
// assert.
//
// distractor_reasons and a "hard" label were both self-reported: the model
// wrote a sentence about why an option was tempting, and nothing could tell a
// real error path from a plausible-sounding number. These gates use the same
// shape the arithmetic gate already uses — the model declares an expression,
// Go evaluates it — so "this wrong option is what a student who forgot to halve
// would get" becomes a claim that can fail.
//
// Both pass trivially when nothing was declared. Whether a slot was *required*
// to declare them is a contract question and lives in the coverage gate, which
// is the only place that can see the slot.

// gateDistractorPath checks every declared wrong-answer expression.
func gateDistractorPath(q Question, ev Evaluator) GateResult {
	res := GateResult{Gate: GateDistractorPath, Deterministic: true}

	declared := 0
	for _, c := range q.Choices {
		if strings.TrimSpace(c.DistractorExpression) != "" {
			declared++
		}
	}
	if declared == 0 {
		res.Pass = true
		res.Reason = "no wrong-answer expressions declared"
		return res
	}

	idx := q.CorrectIndex()
	if idx < 0 {
		res.Reason = "cannot check wrong-answer expressions without exactly one correct choice"
		return res
	}
	if strings.TrimSpace(q.Choices[idx].DistractorExpression) != "" {
		res.Reason = "the correct choice carries a distractor_expression; it is the key, not a wrong answer"
		return res
	}
	if q.Calculation == nil {
		res.Reason = "wrong-answer expressions were declared but the question states no calculation to be wrong about"
		return res
	}
	if ev == nil {
		res.Reason = "wrong-answer expressions declared but no evaluator configured"
		return res
	}

	keyed, err := ev.Eval(q.Calculation.Expression)
	if err != nil {
		res.Reason = fmt.Sprintf("cannot compare wrong answers: key expression %q did not evaluate: %v", q.Calculation.Expression, err)
		return res
	}

	for i, c := range q.Choices {
		expr := strings.TrimSpace(c.DistractorExpression)
		if i == idx || expr == "" {
			continue
		}
		got, err := ev.Eval(expr)
		if err != nil {
			res.Reason = fmt.Sprintf("choice %d wrong-answer expression %q did not evaluate: %v", i+1, expr, err)
			return res
		}
		// A "wrong" path that lands on the keyed value is not a distractor; it
		// is a second correct answer written differently.
		if expectedNearlyEqual(got, keyed) {
			res.Reason = fmt.Sprintf("choice %d wrong-answer expression %q evaluates to %g, the same as the key", i+1, expr, got)
			return res
		}
		// This is the check that makes the whole field worth having: the number
		// printed in the option must be the number that error path produces.
		// Without it the model can attach any expression to any invented value.
		if !choiceMentionsNumber(c.Content, got) {
			res.Reason = fmt.Sprintf("choice %d reads %q but its wrong-answer expression %q evaluates to %g",
				i+1, excerpt(c.Content, 50), expr, got)
			return res
		}
	}

	res.Pass = true
	res.Reason = fmt.Sprintf("%d wrong choice(s) trace to a verified error path", declared)
	return res
}

// gateFlawedWork checks the mistaken work an error-finding stem shows the
// student.
//
// The whole item rests on the displayed attempt actually being wrong, and on
// the wrong result actually being on the page — a stem that says "a student
// calculated 14 N" without ever showing a number the student can check is a
// recall question wearing a costume. Both halves are decidable here: evaluate
// the flawed expression, confirm it differs from the key, confirm the stem
// prints the value it produces.
func gateFlawedWork(q Question, ev Evaluator) GateResult {
	res := GateResult{Gate: GateFlawedWork, Deterministic: true}
	flawed := strings.TrimSpace(q.FlawedExpression)
	if flawed == "" {
		res.Pass = true
		res.Reason = "no flawed work declared"
		return res
	}
	if q.Calculation == nil {
		res.Reason = "flawed work was declared but the question states no correct calculation to contradict it"
		return res
	}
	if ev == nil {
		res.Reason = "flawed work declared but no evaluator configured"
		return res
	}
	keyed, err := ev.Eval(q.Calculation.Expression)
	if err != nil {
		res.Reason = fmt.Sprintf("cannot judge the flawed work: key expression %q did not evaluate: %v", q.Calculation.Expression, err)
		return res
	}
	got, err := ev.Eval(flawed)
	if err != nil {
		res.Reason = fmt.Sprintf("flawed expression %q did not evaluate: %v", flawed, err)
		return res
	}
	if expectedNearlyEqual(got, keyed) {
		res.Reason = fmt.Sprintf("flawed expression %q evaluates to %g, the same as the correct value — there is no mistake to find", flawed, got)
		return res
	}
	if !model.ChoiceMentionsNumber(q.Stem, got) {
		res.Reason = fmt.Sprintf("the stem never shows %g, the result of the flawed work %q, so there is nothing for the student to check", got, flawed)
		return res
	}
	res.Pass = true
	res.Reason = fmt.Sprintf("stem shows the mistaken result %g against a correct %g", got, keyed)
	return res
}

// gateDecoy checks the declared irrelevant givens.
//
// A numeric decoy is verified both ways: it has to be in the stem, and it has
// to be absent from the solution. A non-numeric decoy (a named condition a
// humanities item supplies and does not need) can only be checked for presence
// — there is no expression to be absent from. That asymmetry is deliberate and
// is why the noise axis is only fully enforceable on numeric items.
func gateDecoy(q Question) GateResult {
	res := GateResult{Gate: GateDecoy, Deterministic: true}
	if len(q.DecoyValues) == 0 {
		res.Pass = true
		res.Reason = "no decoy values declared"
		return res
	}

	seen := map[string]bool{}
	used := solutionOperands(q)
	for _, raw := range q.DecoyValues {
		decoy := strings.TrimSpace(raw)
		if decoy == "" {
			res.Reason = "a declared decoy value is empty"
			return res
		}
		key := strings.ToLower(decoy)
		if seen[key] {
			res.Reason = fmt.Sprintf("decoy %q is declared twice; one distraction counts once", decoy)
			return res
		}
		seen[key] = true

		value, numeric := parseDecoy(decoy)
		if !numeric {
			if !strings.Contains(squeeze(strings.ToLower(q.Stem)), squeeze(key)) {
				res.Reason = fmt.Sprintf("declared decoy %q does not appear in the stem", decoy)
				return res
			}
			continue
		}
		if !model.ChoiceMentionsNumber(q.Stem, value) {
			res.Reason = fmt.Sprintf("declared decoy %s is not present in the stem, so it distracts nobody", decoy)
			return res
		}
		for _, operand := range used {
			if expectedNearlyEqual(operand, value) {
				res.Reason = fmt.Sprintf("declared decoy %s is used by the solution %q, so it is a given, not a distraction",
					decoy, q.Calculation.Expression)
				return res
			}
		}
	}

	res.Pass = true
	res.Reason = fmt.Sprintf("%d declared decoy value(s) are in the stem and unused by the solution", len(q.DecoyValues))
	return res
}

// solutionOperands returns the numeric literals the keyed solution actually
// consumes. An empty result means nothing can be proved unused, which the
// caller treats as "presence-only checking".
func solutionOperands(q Question) []float64 {
	if q.Calculation == nil {
		return nil
	}
	literals := expressionNumberPattern.FindAllString(q.Calculation.Expression, -1)
	out := make([]float64, 0, len(literals))
	for _, literal := range literals {
		if v, err := strconv.ParseFloat(literal, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// parseDecoy reads a declared decoy as a number when it is one. Models write
// the value the way the stem writes it, so a unit or a thousands separator
// riding along is normal and is stripped rather than rejected.
func parseDecoy(decoy string) (float64, bool) {
	cleaned := strings.Map(func(r rune) rune {
		if r == ',' || r == ' ' {
			return -1
		}
		return r
	}, decoy)
	fields := strings.FieldsFunc(cleaned, func(r rune) bool {
		return !(r >= '0' && r <= '9') && r != '.' && r != '-' && r != '+' && r != 'e' && r != 'E'
	})
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
