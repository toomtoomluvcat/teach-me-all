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

// TopicKind is what the map step says a topic is.
//
// The classification is made by the model while it still has the passage in
// front of it, which costs no extra request. The alternative — matching known
// teacher-guide phrases against topic titles afterwards — was implemented and
// then removed: it only ever knew the wording of the books it had already seen.
type TopicKind string

const (
	// TopicContent is subject matter a learner is meant to know.
	TopicContent TopicKind = "content"
	// TopicApparatus is teacher-facing machinery about the subject: answer keys,
	// assessment rubrics, scoring guides, test banks, lesson plans, teaching
	// hours. It is real prose, so quote grounding cannot distinguish it.
	TopicApparatus TopicKind = "apparatus"
	// TopicNonContent is page furniture: front matter, tables of contents,
	// headers, indexes.
	TopicNonContent TopicKind = "non_content"
)

// nonContentSentinel is the pre-kind wire value. Cached and older provider
// output still carries it as a topic title.
const nonContentSentinel = "NON_CONTENT"

// Topic is one label the map step attached to a chunk.
type Topic struct {
	Title string    `json:"title"`
	Kind  TopicKind `json:"kind"`
}

// Teaches reports whether this topic may become a concept in the evidence graph.
func (t Topic) Teaches() bool {
	return t.Kind == TopicContent && strings.TrimSpace(t.Title) != ""
}

// UnmarshalJSON accepts a bare string as well as the requested object. A JSON
// schema guarantees syntax, not shape — DeepSeek has already been observed
// returning an older field layout — and a topic that arrives without a kind is
// treated as content, because wrongly dropping real material costs more than
// letting one rubric through to the question-level gate.
func (t *Topic) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, `"`) {
		var title string
		if err := json.Unmarshal(data, &title); err != nil {
			return err
		}
		*t = Topic{Title: title}
		t.normalise()
		return nil
	}

	var wire struct {
		Title string `json:"title"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*t = Topic{Title: wire.Title, Kind: TopicKind(strings.ToLower(strings.TrimSpace(wire.Kind)))}
	t.normalise()
	return nil
}

func (t *Topic) normalise() {
	t.Title = strings.TrimSpace(t.Title)
	if strings.EqualFold(t.Title, nonContentSentinel) {
		t.Title = ""
		t.Kind = TopicNonContent
		return
	}
	switch t.Kind {
	case TopicContent, TopicApparatus, TopicNonContent:
	default:
		t.Kind = TopicContent
	}
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
	ConceptIDs     []string
}

// Outline is the whole course as pass 1 sees it.
type Outline struct {
	CourseTitle   string
	Lessons       []Lesson
	EvidenceGraph *EvidenceGraph
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
	// ChoiceVerdicts is populated by single_defensible so a human can inspect
	// the judge's decision for every option instead of trusting one summary.
	ChoiceVerdicts []ChoiceVerdict `json:"choice_verdicts,omitempty"`
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
