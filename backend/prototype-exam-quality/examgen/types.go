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
	// SourceRole is a deterministic document-structure hint. It is used only
	// to keep intentional pre-learning check statements from becoming evidence.
	SourceRole SourceRole

	// GenerationDirective is an optional internal benchmark instruction. It is
	// not source evidence and is never persisted in the extraction cache.
	GenerationDirective string `json:"-"`

	// LessonID is filled in by pass 1 once the outline exists.
	LessonID string
}

// SourceRole identifies a small number of source sections that must not be
// treated as factual evidence. An empty value means unknown and remains
// backwards-compatible with callers that construct Chunk values directly.
type SourceRole string

const (
	SourceRoleUnknown          SourceRole = ""
	SourceRoleCore             SourceRole = "core"
	SourceRolePrelearningCheck SourceRole = "prelearning_check"
)

// TopicKind is what the map step says a passage is.
//
// The classification is made by the model while it still has the passage in
// front of it, which costs no extra request. The alternative — matching known
// teacher-guide phrases against topic titles afterwards — was implemented and
// then removed: it only ever knew the wording of the books it had already seen.
//
// It is a property of the passage, not of an individual topic. Labelling each
// topic separately was measured first and over-dropped badly: a teacher's-guide
// page mixes instructions with the subject matter they are about, and asking
// "what is this topic" invited the model to file the whole passage under the
// instruction it saw first. "What is this passage" is one judgement per chunk
// and is the question the pipeline actually needs answered.
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

// nonContentSentinel is the pre-kind wire value. Older provider output still
// carries it as a topic title.
const nonContentSentinel = "NON_CONTENT"

// ChunkTopics is what the map step returns for one chunk: what the passage is,
// and the topics it names.
type ChunkTopics struct {
	Kind   TopicKind `json:"kind"`
	Topics []string  `json:"topics"`
}

// NewChunkTopics normalises raw provider fields into a labelled chunk.
func NewChunkTopics(kind string, topics []string) ChunkTopics {
	labelled := ChunkTopics{Kind: TopicKind(strings.ToLower(strings.TrimSpace(kind))), Topics: topics}
	labelled.normalise()
	return labelled
}

// Teaches reports whether this passage may contribute concepts to the evidence
// graph.
func (c ChunkTopics) Teaches() bool { return c.Kind == TopicContent }

// UnmarshalJSON tolerates a bare topic list and the pre-kind NON_CONTENT
// sentinel. A JSON schema guarantees syntax, not shape — DeepSeek has already
// been observed returning an older field layout — and a passage that arrives
// without a kind is read as content, because wrongly dropping source material
// costs more than letting one rubric reach the question-level gates.
func (c *ChunkTopics) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		var topics []string
		if err := json.Unmarshal(data, &topics); err != nil {
			return err
		}
		*c = ChunkTopics{Topics: topics}
		c.normalise()
		return nil
	}

	var wire struct {
		Kind   string   `json:"kind"`
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*c = ChunkTopics{Kind: TopicKind(strings.ToLower(strings.TrimSpace(wire.Kind))), Topics: wire.Topics}
	c.normalise()
	return nil
}

func (c *ChunkTopics) normalise() {
	kept := c.Topics[:0]
	sentinel := false
	for _, title := range c.Topics {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		if strings.EqualFold(title, nonContentSentinel) {
			sentinel = true
			continue
		}
		kept = append(kept, title)
	}
	c.Topics = kept

	switch c.Kind {
	case TopicContent, TopicApparatus, TopicNonContent:
	default:
		c.Kind = TopicContent
	}
	if sentinel && len(c.Topics) == 0 {
		c.Kind = TopicNonContent
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
	Unit       string  `json:"unit,omitempty"`
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

	// Set-generation provenance. The per-chunk path leaves these empty; the
	// set path requires them so a question can be traced to one graph atom and
	// one exact source chunk.
	CoverageSlotID  string `json:"coverage_slot_id,omitempty"`
	EvidenceAtomID  string `json:"evidence_atom_id,omitempty"`
	EvidenceChunkID string `json:"evidence_chunk_id,omitempty"`

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

// GateName identifies which check produced a result.
type GateName string

const (
	GateWellFormed     GateName = "well_formed"
	GateSourceRole     GateName = "source_role"
	GateQuote          GateName = "quote_verbatim"
	GateBlindAnswer    GateName = "answerable_blind"
	GateSourceSpecific GateName = "source_specific"
	GateSingleValid    GateName = "single_defensible"
	GateArithmetic     GateName = "arithmetic"
	GateUnit           GateName = "unit_check"
	GateDistinct       GateName = "not_a_duplicate"
	GateCoverage       GateName = "coverage_contract"
)

// GateResult is the outcome of one check.
type GateResult struct {
	Gate GateName
	Pass bool
	// Reason is always populated on failure and often on success, because the
	// point of the prototype is reading these.
	Reason string
	// Deterministic marks gates that Go decided without asking a model.
	Deterministic bool
	// ChoiceVerdicts is populated by the deferred single_defensible audit.
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
