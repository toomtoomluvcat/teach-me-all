// Package examgen holds the pure logic of the exam-generation pipeline.
//
// Nothing in this package prints, reads the terminal, or talks to a concrete
// model client. It takes interfaces and returns values, so it can be lifted
// into the real backend when the prototype has answered its question.
package examgen

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Chunk is a contiguous slice of the source document, kept with enough
// provenance to point a reader back at the page it came from.
type Chunk struct {
	ID       string
	Page     int
	StartOff int
	EndOff   int
	Text     string

	// LessonID is filled in by pass 1 once the outline exists.
	LessonID string
}

// Lesson is one entry of the course outline produced by pass 1.
type Lesson struct {
	ID      string
	Title   string
	Summary string
	// QuestionBudget is the model's own estimate of how many questions this
	// lesson honestly supports. The MVP does not let the user pick a count;
	// this is the count.
	QuestionBudget int
	ChunkIDs       []string
}

// Outline is the whole course as pass 1 sees it.
type Outline struct {
	CourseTitle string
	Lessons     []Lesson
}

// Kind distinguishes question shapes. The prototype only generates
// KindMCQSingle, but the field exists because the production schema has to
// carry it and we want the generated JSON to already match.
type Kind string

const (
	KindMCQSingle Kind = "mcq_single"
)

// Choice is one option of a multiple-choice question.
type Choice struct {
	Content   string `json:"content"`
	IsCorrect bool   `json:"is_correct"`
}

// Calculation is present only on questions whose answer is arithmetic.
//
// The model is never asked to do the arithmetic. It emits the expression and
// the value it believes that expression produces; Go evaluates the expression
// and gate 4 compares. This is deliberately not a tool-call loop: an expression
// stored in the database can be re-verified years later, a tool call that
// already happened cannot.
type Calculation struct {
	Expression string  `json:"expression"`
	Expected   float64 `json:"expected"`
}

// UnmarshalJSON tolerates a number wrapped in JSON quotes. Some hosted models
// emit that otherwise-valid representation despite being asked for a number.
// It deliberately rejects units and other prose: arithmetic is a correctness
// boundary, not a display field.
func (c *Calculation) UnmarshalJSON(data []byte) error {
	var wire struct {
		Expression string          `json:"expression"`
		Expected   json.RawMessage `json:"expected"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	raw := strings.TrimSpace(string(wire.Expected))
	if raw == "" || raw == "null" {
		return fmt.Errorf("calculation expected is required and must be a number")
	}

	var expected float64
	if raw[0] == '"' {
		var quoted string
		if err := json.Unmarshal(wire.Expected, &quoted); err != nil {
			return fmt.Errorf("calculation expected: %w", err)
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(quoted), 64)
		if err != nil {
			return fmt.Errorf("calculation expected %q must be a number: %w", quoted, err)
		}
		expected = parsed
	} else if err := json.Unmarshal(wire.Expected, &expected); err != nil {
		return fmt.Errorf("calculation expected must be a number: %w", err)
	}
	if math.IsNaN(expected) || math.IsInf(expected, 0) {
		return fmt.Errorf("calculation expected must be finite")
	}

	c.Expression = wire.Expression
	c.Expected = expected
	return nil
}

// Question is what the model is asked to emit, plus what we learn about it.
type Question struct {
	Kind        Kind         `json:"kind"`
	Stem        string       `json:"stem"`
	Choices     []Choice     `json:"choices"`
	Explanation string       `json:"explanation"`
	SourceQuote string       `json:"source_quote"`
	Difficulty  string       `json:"difficulty"`
	Skill       string       `json:"skill"`
	Calculation *Calculation `json:"calculation,omitempty"`

	// Filled in by us, not by the model.
	ChunkID string      `json:"-"`
	Report  *GateReport `json:"-"`
	// Attempt is 0 for a first draft and 1 for one that came back through the
	// repair loop after a deterministic gate rejected it.
	Attempt int `json:"-"`
}

// CorrectIndex returns the index of the single correct choice, or -1 if the
// question does not have exactly one.
func (q Question) CorrectIndex() int {
	found := -1
	for i, c := range q.Choices {
		if !c.IsCorrect {
			continue
		}
		if found != -1 {
			return -1
		}
		found = i
	}
	return found
}

// GateName identifies which of the four checks produced a result.
type GateName string

const (
	GateWellFormed  GateName = "well_formed"
	GateQuote       GateName = "quote_verbatim"
	GateBlindAnswer GateName = "answerable_blind"
	GateSingleValid GateName = "single_defensible"
	GateArithmetic  GateName = "arithmetic"
	GateDistinct    GateName = "not_a_duplicate"
)

// GateResult is the outcome of one check.
type GateResult struct {
	Gate GateName
	Pass bool
	// Reason is always populated on failure and often on success, because the
	// point of the prototype is reading these.
	Reason string
	// Deterministic marks gates that Go decided without asking a model.
	// Gates 1 and 4 are deterministic; 2 and 3 are LLM-as-judge and advisory.
	Deterministic bool
}

// GateReport collects every check run against one question.
type GateReport struct {
	Results []GateResult
}

// Passed reports whether every gate passed.
func (r *GateReport) Passed() bool {
	for _, res := range r.Results {
		if !res.Pass {
			return false
		}
	}
	return true
}

// Repairable reports whether every failure came from a check Go decided by
// itself — a misquote, or arithmetic that does not add up.
//
// Those are worth handing back to the model with the exact discrepancy: the
// question's idea is usually fine and only its citation or its answer key is
// wrong. A question the judge rejected is broken at the level of what it asks,
// and is cheaper to replace than to argue with.
func (r *GateReport) Repairable() bool {
	fails := r.Failures()
	if len(fails) == 0 {
		return false
	}
	for _, f := range fails {
		if !f.Deterministic {
			return false
		}
	}
	return true
}

// Failures returns only the checks that failed.
func (r *GateReport) Failures() []GateResult {
	var out []GateResult
	for _, res := range r.Results {
		if !res.Pass {
			out = append(out, res)
		}
	}
	return out
}

// add appends a result, tolerating a nil report.
func (r *GateReport) add(res GateResult) {
	r.Results = append(r.Results, res)
}
