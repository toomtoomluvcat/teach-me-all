package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Stats accumulates provider-reported model-call timing where available and
// measured request time otherwise, so "the run is slow" can be answered with
// numbers instead of a guess.
//
// The two candidate explanations look identical from the outside — too many
// calls, or each call being slow — and they have opposite fixes. Cutting gates
// helps the first and does nothing for the second; a smaller model or more VRAM
// helps the second and does nothing for the first.
type Stats struct {
	mu        sync.Mutex
	by        map[string]*Bucket
	wallStart time.Time
	wallEnd   time.Time
	now       func() time.Time
}

// Bucket is one labelled kind of call.
type Bucket struct {
	Calls int
	// For Ollama, Total is server-reported wall time, load is model-swap time,
	// prompt is reading the input, and eval is producing the output. Other
	// providers fill Total from measured HTTP request time.
	Total, Load, Prompt, Eval time.Duration
	PromptTokens, EvalTokens  int
}

func NewStats() *Stats { return &Stats{by: map[string]*Bucket{}, now: time.Now} }

func (s *Stats) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Stats) begin() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	if s.wallStart.IsZero() {
		s.wallStart = now
	}
	return now
}

func (s *Stats) end() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wallEnd = s.clock()
}

func (s *Stats) addElapsed(label string, elapsed time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.by[label]
	if b == nil {
		b = &Bucket{}
		s.by[label] = b
	}
	b.Calls++
	b.Total += elapsed
}

func (s *Stats) addTokens(label string, prompt, completion int) {
	if s == nil || (prompt == 0 && completion == 0) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.by[label]
	if b == nil {
		b = &Bucket{}
		s.by[label] = b
	}
	b.PromptTokens += prompt
	b.EvalTokens += completion
}

// The label travels on the context rather than as a parameter. Every call site
// already threads a context and none of them care about the label beyond
// setting it once, so a parameter would be seven signature changes to carry a
// string that only the accounting reads.
type labelKey struct{}

// WithLabel tags every model call made under ctx.
func WithLabel(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, labelKey{}, label)
}

func labelOf(ctx context.Context) string {
	if s, ok := ctx.Value(labelKey{}).(string); ok && s != "" {
		return s
	}
	return "unlabelled"
}

func (s *Stats) add(label string, r *chatResponse) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.by[label]
	if b == nil {
		b = &Bucket{}
		s.by[label] = b
	}
	b.Calls++
	b.Total += time.Duration(r.TotalDuration)
	b.Load += time.Duration(r.LoadDuration)
	b.Prompt += time.Duration(r.PromptEvalDuration)
	b.Eval += time.Duration(r.EvalDuration)
	b.PromptTokens += r.PromptEvalCount
	b.EvalTokens += r.EvalCount
}

// Report renders the breakdown. The column that answers the question is tok/s:
// a 4B Q4 model held entirely in VRAM generates in the tens of tokens per
// second, and a model spilling to system RAM generates in the low single
// digits. If tok/s is healthy and the wall clock is still long, the cost is the
// number of calls, not the hardware.
func (s *Stats) Report() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	labels := make([]string, 0, len(s.by))
	var grand time.Duration
	for l, b := range s.by {
		labels = append(labels, l)
		grand += b.Total
	}
	if len(labels) == 0 {
		return ""
	}
	wall := s.wallEnd.Sub(s.wallStart)
	if wall <= 0 {
		// Keep manually-recorded Stats useful in tests and callers that do not
		// go through the HTTP client. Instrumented client calls always provide a
		// measured wall-clock span.
		wall = grand
	}
	sort.Slice(labels, func(i, j int) bool { return s.by[labels[i]].Total > s.by[labels[j]].Total })

	var w strings.Builder
	fmt.Fprintf(&w, "%-14s %6s %9s %6s %10s %10s %8s\n",
		"call", "count", "total", "share", "in tok", "out tok", "tok/s")
	for _, l := range labels {
		b := s.by[l]
		share := 0.0
		if wall > 0 {
			share = float64(b.Total) / float64(wall) * 100
		}
		rate := 0.0
		if b.Eval > 0 {
			rate = float64(b.EvalTokens) / b.Eval.Seconds()
		}
		fmt.Fprintf(&w, "%-14s %6d %9s %5.0f%% %10d %10d %8.1f\n",
			l, b.Calls, round(b.Total), share, b.PromptTokens, b.EvalTokens, rate)
	}

	var loads time.Duration
	var calls int
	for _, b := range s.by {
		loads += b.Load
		calls += b.Calls
	}
	fmt.Fprintf(&w, "%-14s %6d %9s\n", "TOTAL", calls, round(grand))
	fmt.Fprintf(&w, "model wall clock %s\n", round(wall))
	if loads > time.Second {
		fmt.Fprintf(&w, "model loading accounted for %s of that\n", round(loads))
	}
	return w.String()
}

func round(d time.Duration) string {
	if d > time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Millisecond).String()
}
