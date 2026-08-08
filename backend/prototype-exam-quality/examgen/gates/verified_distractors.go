package gates

import (
	"fmt"
	"math"
	"regexp"
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

// isErrorFinding reports whether the question asks the student to locate a
// mistake rather than to produce the answer. Spelled out here rather than
// imported so the gates package keeps deciding questions from the question.
func isErrorFinding(q Question) bool {
	switch strings.ToLower(strings.TrimSpace(q.Skill)) {
	case "error-finding", "error_finding", "error finding", "errorfinding":
		return true
	}
	return strings.TrimSpace(q.FlawedExpression) != ""
}

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
		if !distractorMentionsNumber(c.Content, got) {
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

// distractorRounding is how far a wrong option may sit from the value its
// error path produces. It is looser than the answer-key matcher on purpose.
//
// The key's precision is part of the question: a student has to recognise the
// right number, so "0.42" for a computed 0.41667 is a different answer unless
// the choice says it is approximate. A distractor carries no such duty. Its
// only job is to be a number a mistaken student would arrive at, and whether
// the writer prints 0.5988 or 0.60 changes nothing about that. Holding wrong
// options to the key's bar rejected four otherwise-sound calculation items in
// one run — every one of them for rounding, none for being invented.
//
// 2% is wide enough for two-significant-figure rounding at any scale and still
// far tighter than the gap between a real error path and a made-up number,
// which also has to clear "must not equal the key" to get here at all.
const distractorRounding = 0.02

func distractorMentionsNumber(choice string, v float64) bool {
	if choiceMentionsNumber(choice, v) {
		return true
	}
	if v == 0 {
		return false
	}
	for _, token := range expressionNumberPattern.FindAllString(choice, -1) {
		printed, err := strconv.ParseFloat(token, 64)
		if err != nil {
			continue
		}
		if math.Abs(printed-v)/math.Abs(v) <= distractorRounding {
			return true
		}
	}
	return false
}

// expressionSymbolReplacer folds the characters a writer reaches for in prose
// onto the alphabet Arith accepts. Arith stays deliberately tiny — it will
// re-evaluate stored expressions for years and a grammar with no identifiers
// cannot be talked into anything — so the folding happens out here, before the
// gates, rather than by widening the parser.
var expressionSymbolReplacer = strings.NewReplacer(
	"×", "*", "·", "*", "⋅", "*", "∙", "*",
	"÷", "/", "−", "-", "–", "-", "—", "-",
	"⁄", "/", " ", " ",
)

// NormalizeExpressions rewrites the declared arithmetic into the evaluator's
// alphabet where that can be done without changing what it says.
//
// Two shapes, both lossless. The first is typographic: Arith already takes ×
// and ÷, but not the typographic minus "−", the en dash, the middle dot, or the
// fraction slash, all of which a writer produces without meaning anything
// different by them.
//
// The second is a symbolic lead-in. Models like to show their reasoning inside
// the field — "a = F_net / m = 100 / 5" — and the last segment is the arithmetic
// they actually mean; everything before it is the derivation. Keeping the final
// segment that parses is a rewrite of notation, not of content.
//
// What this deliberately does not do is rescue an expression with no numbers in
// it at all. "Fnet/m" and "4T-f" were both seen in one run, and there is nothing
// to recover: supplying values for the symbols would be inventing the
// calculation rather than repairing it. Those still fail, which is the only
// honest outcome.
func NormalizeExpressions(q Question, ev Evaluator) Question {
	if ev == nil {
		return q
	}
	if q.Calculation != nil {
		q.Calculation.Expression = normalizeExpression(q.Calculation.Expression, ev)
	}
	q.FlawedExpression = normalizeExpression(q.FlawedExpression, ev)
	if len(q.Choices) > 0 {
		normalized := append([]Choice(nil), q.Choices...)
		for i := range normalized {
			normalized[i].DistractorExpression = normalizeExpression(normalized[i].DistractorExpression, ev)
		}
		q.Choices = normalized
	}
	return q
}

func normalizeExpression(expr string, ev Evaluator) string {
	original := expr
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return original
	}
	if _, err := ev.Eval(trimmed); err == nil {
		return original
	}

	folded := strings.TrimSpace(expressionSymbolReplacer.Replace(trimmed))
	if _, err := ev.Eval(folded); err == nil {
		return folded
	}
	// Right to left: the rightmost parsable segment of "a = F/m = 100/5" is the
	// arithmetic; anything further left is the derivation that led to it.
	if segments := strings.Split(folded, "="); len(segments) > 1 {
		for i := len(segments) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(segments[i])
			if candidate == "" {
				continue
			}
			if _, err := ev.Eval(candidate); err == nil {
				return candidate
			}
		}
	}
	return original
}

// stemEquationPattern finds a worked line an error-finding stem displays:
// some arithmetic, an equals sign, and the number it was claimed to produce.
// The left side must carry an operator, so a bare restatement ("mass = 5.0 kg")
// is not mistaken for work.
var stemEquationPattern = regexp.MustCompile(`([0-9][0-9\s.()]*(?:[-+*/^][0-9\s.()]*)+)=\s*(-?[0-9]+(?:\.[0-9]+)?)`)

// stemArithmeticReplacer maps the characters a writer uses in prose onto the
// alphabet the evaluator accepts. Nothing else is normalised: a stem that
// writes its work in words has no equation to recover and should say so by
// failing, not by being guessed at.
var stemArithmeticReplacer = strings.NewReplacer(
	"×", "*", "·", "*", "⋅", "*", "x", "*", "X", "*",
	"÷", "/", "−", "-", "–", "-", "—", "-", ",", "",
)

// RepairErrorFinding fixes the two things an error-finding draft reliably gets
// wrong, and rebuilds an error-finding question's flawed arithmetic from
// the work its own stem displays.
//
// Measured across four runs and two rounds of prompt sharpening: the writer
// fills flawed_expression with the correct arithmetic — the same string it just
// put in calculation.expression — in roughly half of all error-finding drafts.
// Telling it not to did not move the number, which is the same shape as the
// skill-rotation finding: a field the model fills by copying is not fixed by
// asking it to copy something else.
//
// It does not have to be asked. An error-finding stem must print the mistaken
// work for the student to inspect, so the flawed arithmetic is already on the
// page as text. Reading "2100*49+650 = 103550" out of the stem and evaluating
// it is strictly better evidence than the model's own claim about what it did:
// the equation is what the student actually sees, and it is only accepted when
// the left side really does produce the right side and that result really does
// differ from the correct value. A stem with no such equation recovers nothing
// and still fails, which is correct — there was nothing for the student to
// check either.
func RepairErrorFinding(q Question, ev Evaluator) Question {
	if !isErrorFinding(q) {
		return q
	}
	// An error-finding item's options are diagnoses — "the force was multiplied
	// instead of divided" — not numbers, so a wrong-answer expression has no
	// number in the option to match and the writer attaches one anyway. Every
	// draft in one run failed on exactly that. The expression is meaningless
	// here rather than wrong, so it is dropped: the option set is judged on
	// distractor_atom_id or written reasons like any other prose item, and the
	// axis tally does not get to count paths that were never checkable.
	stripped := append([]Choice(nil), q.Choices...)
	for i := range stripped {
		stripped[i].DistractorExpression = ""
	}
	q.Choices = stripped

	if q.Calculation == nil || ev == nil {
		return q
	}
	keyed, err := ev.Eval(q.Calculation.Expression)
	if err != nil {
		return q
	}
	// Only step in when the declared work is missing or is not wrong at all.
	// A writer that got it right is left alone.
	if declared := strings.TrimSpace(q.FlawedExpression); declared != "" {
		if got, err := ev.Eval(declared); err == nil && !expectedNearlyEqual(got, keyed) {
			return q
		}
	}

	stem := stemArithmeticReplacer.Replace(q.Stem)
	for _, match := range stemEquationPattern.FindAllStringSubmatch(stem, -1) {
		expr := strings.TrimSpace(match[1])
		claimed, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			continue
		}
		got, err := ev.Eval(expr)
		if err != nil {
			continue
		}
		// The stem has to be internally honest about what its work produced,
		// and that result has to be the wrong one. Either failing means this
		// line is not the mistake the question is about.
		if !expectedNearlyEqual(got, claimed) || expectedNearlyEqual(got, keyed) {
			continue
		}
		q.FlawedExpression = expr
		return q
	}
	return q
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
		key := strings.ToLower(decoy)
		if decoy != "" && seen[key] {
			res.Reason = fmt.Sprintf("decoy %q is declared twice; one distraction counts once", decoy)
			return res
		}
		seen[key] = true
		if fault := decoyFault(q, decoy, used); fault != "" {
			res.Reason = fault
			return res
		}
	}

	res.Pass = true
	res.Reason = fmt.Sprintf("%d declared decoy value(s) are in the stem and unused by the solution", len(q.DecoyValues))
	return res
}

// decoyFault returns why this single declared decoy is not one, or "".
//
// Split out from the gate so the repair pass can ask the same question about
// one decoy at a time without duplicating the rules.
func decoyFault(q Question, decoy string, used []float64) string {
	decoy = strings.TrimSpace(decoy)
	if decoy == "" {
		return "a declared decoy value is empty"
	}
	value, numeric := parseDecoy(decoy)
	if !numeric {
		if !strings.Contains(squeeze(strings.ToLower(q.Stem)), squeeze(strings.ToLower(decoy))) {
			return fmt.Sprintf("declared decoy %q does not appear in the stem", decoy)
		}
		return ""
	}
	if !model.ChoiceMentionsNumber(q.Stem, value) {
		return fmt.Sprintf("declared decoy %s is not present in the stem, so it distracts nobody", decoy)
	}
	for _, operand := range used {
		if expectedNearlyEqual(operand, value) {
			return fmt.Sprintf("declared decoy %s is used by the solution %q, so it is a given, not a distraction",
				decoy, q.Calculation.Expression)
		}
	}
	return ""
}

// RepairDecoyValues drops a declared decoy that is not one, instead of letting
// it take the question down with it.
//
// A decoy is a claim about the stem — "this given is here and the answer does
// not need it" — and a wrong claim about the stem is a labelling defect, the
// same shape as a distractor citing the wrong atom. Failing the whole item over
// it costs a question to punish a field, and it costs it even when the slot
// never asked for a decoy and the writer volunteered a bad one unprompted,
// which happens on plain recall and calculation items too.
//
// After the drop the question is judged on what survives: a slot that did ask
// for decoys now fails at the coverage gate with "requires 1 decoy value, got
// 0", which says the honest thing — the stem had nothing spare in it — rather
// than blaming the label. Nothing is invented; a decoy that is really in the
// stem and really unused is kept exactly as written.
func RepairDecoyValues(q Question) Question {
	if len(q.DecoyValues) == 0 {
		return q
	}
	used := solutionOperands(q)
	seen := map[string]bool{}
	kept := make([]string, 0, len(q.DecoyValues))
	for _, raw := range q.DecoyValues {
		decoy := strings.TrimSpace(raw)
		key := strings.ToLower(decoy)
		if seen[key] || decoyFault(q, decoy, used) != "" {
			continue
		}
		seen[key] = true
		kept = append(kept, raw)
	}
	if len(kept) == len(q.DecoyValues) {
		return q
	}
	if len(kept) == 0 {
		q.DecoyValues = nil
		return q
	}
	q.DecoyValues = kept
	return q
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
