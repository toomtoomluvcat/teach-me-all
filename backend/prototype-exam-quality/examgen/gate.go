package examgen

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// BlindVerdict is what a judge reports when it sees only the question — no
// source material.
type BlindVerdict struct {
	// Interpretable answers "reading only this, is it clear what is being
	// asked?". This is the vagueness detector and the reason gate 2 exists.
	Interpretable bool   `json:"interpretable"`
	Reason        string `json:"reason"`
}

type SourceDependency string

const (
	SourceDependencySpecific SourceDependency = "specific"
	SourceDependencyGeneric  SourceDependency = "generic"
	SourceDependencyUnclear  SourceDependency = "unclear"
)

type DependencyKind string

const (
	DependencyNumber          DependencyKind = "number"
	DependencyNamedStructure  DependencyKind = "named_structure"
	DependencyOrder           DependencyKind = "order"
	DependencyCondition       DependencyKind = "condition"
	DependencyCauseEffect     DependencyKind = "cause_effect"
	DependencyComparison      DependencyKind = "comparison"
	DependencyLocalDefinition DependencyKind = "local_definition"
	DependencyNone            DependencyKind = "none"
)

// SourcedVerdict is what a judge reports when it can see the source chunk.
type SourcedVerdict struct {
	BestIndex        int              `json:"best_index"`
	AlsoDefensible   []int            `json:"also_defensible"`
	ChoiceVerdicts   []ChoiceVerdict  `json:"choice_verdicts"`
	Reason           string           `json:"reason"`
	SourceDependency SourceDependency `json:"dependency"`
	DependencyKind   DependencyKind   `json:"dependency_kind"`
	Evidence         []string         `json:"evidence"`
	Counterfactual   bool             `json:"counterfactual"`
	DependencyReason string           `json:"dependency_reason"`
}

type ChoiceStatus string

const (
	ChoiceSupported   ChoiceStatus = "supported"
	ChoiceUnsupported ChoiceStatus = "unsupported"
	ChoiceEquivalent  ChoiceStatus = "equivalent"
	ChoiceAmbiguous   ChoiceStatus = "ambiguous"
)

// ChoiceVerdict forces the source judge to audit every option rather than
// making one holistic pick and overlooking a paraphrased correct answer.
type ChoiceVerdict struct {
	Index  int          `json:"index"`
	Status ChoiceStatus `json:"status"`
	Reason string       `json:"reason"`
}

// Judge is the model-backed half of the source-dependency gate. The pipeline
// never lets it see the generator's reasoning.
type Judge interface {
	JudgeAgainstSource(ctx context.Context, q Question, source string) (SourcedVerdict, error)
}

// Evaluator evaluates an arithmetic expression. Implemented in calc.go.
type Evaluator interface {
	Eval(expr string) (float64, error)
}

// minQuoteRunes stops a model from satisfying gate 1 with a quote so short it
// matches by accident. Anything shorter is not evidence of grounding.
const minQuoteRunes = 25

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
			if digits >= 2 {
				return minNumericQuoteRunes
			}
		}
	}
	return minQuoteRunes
}

// arithmeticTolerance is relative; expressions producing money or physical
// quantities get rounded by the model in the choice text.
const arithmeticTolerance = 1e-6

// RunGates applies the cheap checks plus the source-dependency check to one
// question and returns the report.
// It never returns an error for a failing question — failure is data. It
// returns an error only when the judge itself is unreachable.
func RunGates(ctx context.Context, q Question, chunk Chunk, j Judge, ev Evaluator) (*GateReport, error) {
	rep := RunCheapGates(q, chunk, ev)
	if err := AddJudgeGates(ctx, rep, q, chunk, j); err != nil {
		return nil, err
	}
	return rep, nil
}

// RunCheapGates runs everything Go can decide by itself. No model, no network,
// microseconds.
func RunCheapGates(q Question, chunk Chunk, ev Evaluator) *GateReport {
	rep := &GateReport{}
	rep.add(gateWellFormed(q))
	rep.add(gateQuote(q, chunk))
	rep.add(gateArithmetic(q, ev))
	return rep
}

// AddJudgeGates appends the one model-backed verdict this experiment needs:
// whether the answer depends on a fact specific to the source passage. The
// blind-clarity and per-choice semantic audits remain available for a later
// quality pass, but are deliberately out of this measurement path.
//
// This ordering is the cheapest optimisation available. The judges are 45% of
// wall clock, and on a measured run 7 questions were duplicates and 4 misquoted
// the source; every one of those was sent to two judges anyway, purely to
// confirm a verdict Go had already reached. A question that fails a check Go is
// certain about cannot be rescued by a judge that likes it.
func AddJudgeGates(ctx context.Context, rep *GateReport, q Question, chunk Chunk, j Judge) error {
	if len(rep.Failures()) > 0 {
		rep.add(GateResult{Gate: GateSourceSpecific, Pass: true, Reason: "not asked — already rejected by a deterministic check"})
		return nil
	}

	sourced, err := j.JudgeAgainstSource(ctx, q, chunk.Text)
	if err != nil {
		rep.add(GateResult{Gate: GateSourceSpecific, Reason: "judge did not answer: " + err.Error()})
		return nil
	}
	rep.add(gateSourceSpecific(q, chunk, sourced))

	return nil
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

	if q.Calculation == nil {
		res.Pass = true
		res.Reason = "no arithmetic in this question"
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
	candidates := []string{
		trimZeros(fmt.Sprintf("%.6f", v)),
		trimZeros(fmt.Sprintf("%.4f", v)),
		trimZeros(fmt.Sprintf("%.2f", v)),
		trimZeros(fmt.Sprintf("%.1f", v)),
		fmt.Sprintf("%.0f", v),
		fmt.Sprintf("%g", v),
	}
	// Strip thousands separators so "1,234" matches "1234".
	haystack := strings.ReplaceAll(choice, ",", "")
	for _, c := range candidates {
		if c != "" && strings.Contains(haystack, c) {
			return true
		}
	}
	return false
}

func trimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
