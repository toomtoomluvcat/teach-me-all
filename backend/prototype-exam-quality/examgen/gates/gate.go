package gates

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"protoexam/examgen/model"
)

// Evaluator evaluates an arithmetic expression. Implemented in calc.go.
type Evaluator interface {
	Eval(expr string) (float64, error)
}

// minQuoteRunes stops a model from satisfying the QC with a one-word quote,
// while allowing short exact named facts such as "ไฮดรา พลานาเรีย".
const minQuoteRunes = 12

// minNumericQuoteRunes is the floor for a quote carrying numbers. A worked
// calculation cites itself in very few characters — "1200 * 0.07 = 84 baht" is
// 22 runes and is more specific evidence than any sentence of prose. Holding
// numeric citations to the prose floor threw away good questions.
const minNumericQuoteRunes = 12

// quoteFloor picks the length floor for a quote. Two or more digits means the
// quote points at particular numbers, which cannot match by accident.
func quoteFloor(quote string) int {
	digits := 0
	for _, r := range quote {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	// A compact equation such as "6^3" or "2-8" is more specific than a
	// prose fragment even when it is only a few runes long. Do not discard a
	// good numeric item merely because PDF extraction made the evidence terse.
	if digits >= 2 && strings.ContainsAny(quote, "=^*/+-×") {
		return 3
	}
	if digits >= 2 {
		return minNumericQuoteRunes
	}
	return minQuoteRunes
}

// arithmeticTolerance is relative. A small absolute allowance below accepts
// ordinary three-decimal rounding for results >= 1 without making tiny
// scientific-notation answers effectively uncheckable.
const arithmeticTolerance = 1e-6
const roundedAnswerTolerance = 5e-4

// RunCheapGates runs everything Go can decide by itself. No model, no network,
// microseconds.
func RunCheapGates(q Question, chunk Chunk, ev Evaluator) *GateReport {
	rep := &GateReport{}
	rep.Results = append(rep.Results, gateWellFormed(q))
	rep.Results = append(rep.Results, gateSourceRole(chunk))
	rep.Results = append(rep.Results, gateQuote(q, chunk))
	rep.Results = append(rep.Results, gateArithmetic(q, ev))
	rep.Results = append(rep.Results, gateUnitCheck(q))
	rep.Results = append(rep.Results, gateDemandContract(q))
	return rep
}

// gateDemandContract is a compact deterministic check for the claimed
// cognitive load. It does not prove the item is good; it blocks the cheaper
// failure where a hard/application label has no explicit changed condition,
// support steps, or distractor rationale at all.
func gateDemandContract(q Question) GateResult {
	res := GateResult{Gate: GateDemand, Deterministic: true}
	difficulty := strings.ToLower(strings.TrimSpace(q.Difficulty))
	skill := strings.ToLower(strings.TrimSpace(q.Skill))
	if skill != "application" || (difficulty != "medium" && difficulty != "hard") {
		res.Pass = true
		res.Reason = "no medium/hard application demand contract required"
		return res
	}
	if isGenericChangedCondition(q.ChangedCondition) {
		res.Reason = "application demand contract is missing a specific changed condition"
		return res
	}
	if len(q.DistractorReasons) != len(q.Choices)-1 {
		res.Reason = fmt.Sprintf("application %s needs one distractor_reason per wrong choice, got %d for %d choices", difficulty, len(q.DistractorReasons), len(q.Choices)-1)
		return res
	}
	for i, reason := range q.DistractorReasons {
		if model.RuneLen(strings.TrimSpace(reason)) < 8 {
			res.Reason = fmt.Sprintf("distractor_reason %d is too short to name a wrong assumption", i+1)
			return res
		}
	}
	if difficulty == "hard" && !validDemandSteps(q.ReasoningSteps) {
		res.Reason = "hard application needs at least two distinct concrete reasoning_steps"
		return res
	}
	res.Pass = true
	res.Reason = fmt.Sprintf("%s application demand contract is present", difficulty)
	return res
}

func validDemandSteps(steps []string) bool {
	if len(steps) < 2 {
		return false
	}
	seen := map[string]bool{}
	for _, step := range steps {
		key := strings.ToLower(squeeze(step))
		if model.RuneLen(key) < 8 || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func isGenericChangedCondition(condition string) bool {
	lower := strings.ToLower(squeeze(condition))
	if lower == "" {
		return true
	}
	for _, generic := range []string{
		"changed condition", "different condition", "a changed condition",
		"เปลี่ยนเงื่อนไข", "เงื่อนไขที่เปลี่ยน", "เงื่อนไขใหม่",
	} {
		if lower == generic {
			return true
		}
	}
	return false
}

// gateSourceRole is a narrow structural QC check. It does not decide whether
// a question is educationally good; it only blocks a quote from a section
// whose statements are explicitly presented for pre-learning checking.
func gateSourceRole(chunk Chunk) GateResult {
	res := GateResult{Gate: GateSourceRole, Deterministic: true}
	role := chunk.SourceRole
	if role == SourceRoleUnknown {
		role = classifySourceRole(chunk.Text)
	}
	if role == SourceRolePrelearningCheck {
		res.Reason = fmt.Sprintf("chunk %s, page %d is a pre-learning check/answer-key section", chunk.ID, chunk.Page)
		return res
	}

	res.Pass = true
	res.Reason = fmt.Sprintf("chunk %s, page %d is usable source text", chunk.ID, chunk.Page)
	return res
}

// gateQuote is deterministic: the quote the model attributed to the source has
// to actually be in the source.
//
// Whitespace is removed from both sides before comparing, not merely collapsed.
// PDF extraction sprinkles spaces between Thai syllables — Thai does not put
// spaces between words, so every one of those is an artefact — and the model
// writes the same sentence without them. Measured on a real Thai handout, that
// alone failed 12 of 15 otherwise-correct quotes.
//
// Nothing else is normalised. Characters and their order must still match
// exactly, so a model that transcribes "Creativity" as "Cretivity" still fails,
// which is the case this gate exists to catch.
func gateQuote(q Question, chunk Chunk) GateResult {
	res := GateResult{Gate: GateQuote, Deterministic: true}

	quote := stripQuoteMarks(q.SourceQuote)
	floor := quoteFloor(quote)
	if n := RuneLen(collapseWS(quote)); n < floor {
		res.Reason = fmt.Sprintf("quote is %d runes, minimum is %d — too short to be evidence", n, floor)
		return res
	}
	if !strings.Contains(squeeze(chunk.Text), squeeze(quote)) {
		res.Reason = fmt.Sprintf("quote not found verbatim in chunk %s (page %d)", chunk.ID, chunk.Page)
		return res
	}

	res.Pass = true
	res.Reason = fmt.Sprintf("verbatim in chunk %s, page %d", chunk.ID, chunk.Page)
	return res
}

// gateArithmetic is deterministic: Go evaluates the expression, not the model.
// Questions with no calculation pass trivially.
func gateArithmetic(q Question, ev Evaluator) GateResult {
	res := GateResult{Gate: GateArithmetic, Deterministic: true}

	if !q.NeedsCalculation() {
		res.Pass = true
		res.Reason = "no arithmetic in this question"
		return res
	}
	if q.Calculation == nil {
		res.Reason = "requires_calculation=true but calculation.expression/expected is missing"
		return res
	}
	if ev == nil {
		res.Reason = "calculation present but no evaluator configured"
		return res
	}

	got, err := ev.Eval(q.Calculation.Expression)
	if err != nil {
		res.Reason = fmt.Sprintf("expression %q did not evaluate: %v", q.Calculation.Expression, err)
		return res
	}
	if !nearlyEqual(got, q.Calculation.Expected) {
		res.Reason = fmt.Sprintf("expression %q evaluates to %g but the model stated %g",
			q.Calculation.Expression, got, q.Calculation.Expected)
		return res
	}

	// The stated value also has to be the choice marked correct, otherwise the
	// arithmetic is right and the answer key is still wrong.
	idx := q.CorrectIndex()
	if idx < 0 {
		res.Reason = "cannot check the answer key: question does not have exactly one correct choice"
		return res
	}
	if !choiceMentionsNumber(q.Choices[idx].Content, got) {
		res.Reason = fmt.Sprintf("expression evaluates to %g but the correct choice reads %q",
			got, q.Choices[idx].Content)
		return res
	}

	res.Pass = true
	res.Reason = fmt.Sprintf("%s = %g, matches the correct choice", q.Calculation.Expression, got)
	return res
}

// gateUnitCheck is the lightweight unit tool. It validates the model's
// declared answer unit and checks that the keyed choice carries the same unit.
// It deliberately does not guess missing units: a dimensionless ratio or a
// numeric answer without a source-stated unit is allowed to declare an empty
// unit. The accepted alphabet is intentionally subject-neutral so units such
// as mol/L, %, kJ/mol, °C, and USD are not treated as physics-only.
func gateUnitCheck(q Question) GateResult {
	res := GateResult{Gate: GateUnit, Deterministic: true}
	if q.Calculation == nil {
		res.Pass = true
		res.Reason = "no arithmetic in this question"
		return res
	}
	unit := strings.TrimSpace(q.Calculation.Unit)
	if unit == "" {
		res.Pass = true
		res.Reason = "dimensionless or no source-stated unit declared"
		return res
	}
	normal, ok := normaliseUnit(unit)
	if !ok {
		res.Reason = fmt.Sprintf("unit %q contains unsupported symbols or malformed exponents", unit)
		return res
	}
	idx := q.CorrectIndex()
	if idx < 0 {
		res.Reason = "cannot check answer unit without exactly one correct choice"
		return res
	}
	if !choiceHasUnit(q.Choices[idx].Content, normal) {
		res.Reason = fmt.Sprintf("declared answer unit %q is missing from the correct choice %q", unit, q.Choices[idx].Content)
		return res
	}
	res.Pass = true
	res.Reason = fmt.Sprintf("correct choice carries answer unit %s", normal)
	return res
}

func normaliseUnit(unit string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(unit))
	s = strings.ReplaceAll(s, "²", "^2")
	s = strings.ReplaceAll(s, "³", "^3")
	s = strings.ReplaceAll(s, "⁻", "-")
	s = strings.ReplaceAll(s, "−", "-")
	s = strings.ReplaceAll(s, "·", "*")
	s = strings.ReplaceAll(s, "×", "*")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return "", true
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("*/^.-+%°()", r) {
			continue
		}
		return "", false
	}
	if strings.HasPrefix(s, "*") || strings.HasSuffix(s, "*") || strings.Contains(s, "**") || strings.Contains(s, "//") {
		return "", false
	}
	return s, true
}

func choiceHasUnit(choice, unit string) bool {
	normalChoice := strings.ToLower(choice)
	normalChoice = strings.ReplaceAll(normalChoice, "²", "^2")
	normalChoice = strings.ReplaceAll(normalChoice, "³", "^3")
	normalChoice = strings.ReplaceAll(normalChoice, "−", "-")
	normalChoice = strings.ReplaceAll(normalChoice, "·", "*")
	normalChoice = strings.ReplaceAll(normalChoice, "×", "*")
	normalChoice = strings.ReplaceAll(normalChoice, " ", "")
	if strings.Contains(normalChoice, unit) {
		return true
	}
	switch unit {
	case "n":
		return strings.Contains(normalChoice, "newton")
	case "kg*m/s^2":
		return strings.Contains(normalChoice, "n") || strings.Contains(normalChoice, "newton")
	case "m/s^2":
		return strings.Contains(normalChoice, "m/s^2") || strings.Contains(normalChoice, "m·s^-2")
	case "m/s":
		return strings.Contains(normalChoice, "m/s")
	default:
		return false
	}
}

// gateInterpretable is the vagueness check. It asks whether a reader can tell
// what is being asked without the source in front of them — not whether they
// could answer it.
func gateInterpretable(q Question, v BlindVerdict) GateResult {
	res := GateResult{Gate: GateBlindAnswer}

	if !v.Interpretable {
		res.Reason = "judge could not tell what is being asked: " + v.Reason
		return res
	}

	res.Pass = true
	res.Reason = "question is self-contained"
	return res
}

// gateSourceSpecific checks whether the source judge identified a fact that is
// specific to this passage, rather than asking whether the model happened to
// know the answer before seeing it. The model supplies the semantic judgement;
// Go verifies that its evidence is really in the cited chunk.
func gateSourceSpecific(_ Question, chunk Chunk, v SourcedVerdict) GateResult {
	res := GateResult{Gate: GateSourceSpecific}

	if v.SourceDependency == "" {
		res.Reason = "NOT JUDGED — source judge omitted the source-dependency verdict or evidence"
		return res
	}
	if v.SourceDependency != SourceDependencySpecific {
		res.Reason = fmt.Sprintf("source dependency is %q, not specific", v.SourceDependency)
		return res
	}
	if len(v.Evidence) == 0 {
		res.Reason = "NOT JUDGED — source-specific verdict omitted evidence"
		return res
	}
	for _, evidence := range v.Evidence {
		evidence = stripQuoteMarks(strings.TrimSpace(evidence))
		if evidence == "" {
			res.Reason = "source judge supplied an empty evidence span"
			return res
		}
		if !strings.Contains(squeeze(chunk.Text), squeeze(evidence)) {
			res.Reason = fmt.Sprintf("source evidence is not verbatim in chunk %s: %q", chunk.ID, evidence)
			return res
		}
	}

	res.Pass = true
	res.Reason = fmt.Sprintf("source-specific fact supported by %d verified evidence span(s)", len(v.Evidence))
	return res
}

// gateSingleDefensible checks the answer key against a judge that can see the
// source: the judge's pick has to match, and no other choice may be arguable.
func gateSingleDefensible(q Question, v SourcedVerdict) GateResult {
	res := GateResult{Gate: GateSingleValid, ChoiceVerdicts: v.ChoiceVerdicts}

	idx := q.CorrectIndex()
	if idx < 0 {
		res.Reason = "question does not mark exactly one choice correct"
		return res
	}
	if v.BestIndex != idx {
		res.Reason = fmt.Sprintf("answer key says choice %d, judge reading the source says choice %d: %s",
			idx+1, v.BestIndex+1, v.Reason)
		return res
	}
	if len(v.AlsoDefensible) > 0 {
		var others []string
		for _, i := range v.AlsoDefensible {
			if i == idx {
				continue
			}
			others = append(others, fmt.Sprintf("%d", i+1))
		}
		if len(others) > 0 {
			res.Reason = fmt.Sprintf("choice %s also defensible from the source: %s",
				strings.Join(others, ", "), v.Reason)
			return res
		}
	}
	if len(v.ChoiceVerdicts) > 0 {
		byIndex := make(map[int]ChoiceVerdict, len(v.ChoiceVerdicts))
		for _, verdict := range v.ChoiceVerdicts {
			if verdict.Index < 0 || verdict.Index >= len(q.Choices) {
				res.Reason = fmt.Sprintf("source judge returned out-of-range choice index %d", verdict.Index)
				return res
			}
			if _, duplicate := byIndex[verdict.Index]; duplicate {
				res.Reason = fmt.Sprintf("source judge evaluated choice %d more than once", verdict.Index+1)
				return res
			}
			byIndex[verdict.Index] = verdict
		}
		if len(byIndex) != len(q.Choices) {
			res.Reason = fmt.Sprintf("source judge evaluated %d of %d choices", len(byIndex), len(q.Choices))
			return res
		}
		for i := range q.Choices {
			verdict := byIndex[i]
			if i == idx && verdict.Status != ChoiceSupported {
				res.Reason = fmt.Sprintf("choice %d is marked correct but audited as %s: %s", i+1, verdict.Status, verdict.Reason)
				return res
			}
			if i != idx && verdict.Status != ChoiceUnsupported {
				res.Reason = fmt.Sprintf("choice %d is a distractor but audited as %s: %s", i+1, verdict.Status, verdict.Reason)
				return res
			}
		}
	}

	res.Pass = true
	res.Reason = "exactly one choice holds up after auditing every option against the source"
	return res
}

func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// squeeze removes every whitespace character. See gateQuote for why this is
// the right comparison and what it deliberately does not forgive.
func squeeze(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// stripQuoteMarks removes wrapping quotation marks. Models like to hand back
// the quote already quoted, and the marks are not part of the source.
func stripQuoteMarks(s string) string {
	s = strings.TrimSpace(s)
	for {
		r := []rune(s)
		if len(r) < 2 {
			return s
		}
		first, last := r[0], r[len(r)-1]
		if isQuoteMark(first) && isQuoteMark(last) {
			s = strings.TrimSpace(string(r[1 : len(r)-1]))
			continue
		}
		return s
	}
}

func isQuoteMark(r rune) bool {
	switch r {
	case '"', '\'', '“', '”', '‘', '’', '«', '»', '「', '」':
		return true
	}
	return false
}

func nearlyEqual(a, b float64) bool {
	if a == b {
		return true
	}
	if math.Abs(a) >= 1 && math.Abs(b) >= 1 && math.Abs(a-b) <= roundedAnswerTolerance {
		return true
	}
	scale := math.Max(math.Abs(a), math.Abs(b))
	if scale == 0 {
		return true
	}
	return math.Abs(a-b)/scale < arithmeticTolerance
}

// choiceMentionsNumber checks the correct choice actually contains the computed
// value. Choices are written by the model as prose ("about 7 บาทต่อเดือน"), so
// this compares against several roundings rather than demanding an exact string.
func choiceMentionsNumber(choice string, v float64) bool {
	return modelChoiceMentionsNumber(choice, v)
}

func modelChoiceMentionsNumber(choice string, v float64) bool {
	return model.ChoiceMentionsNumber(choice, v)
}
