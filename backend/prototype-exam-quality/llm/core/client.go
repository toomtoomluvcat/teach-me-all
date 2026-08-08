// Package llm is a thin Ollama client. No SDK: the prototype needs four
// endpoints and pulling in the whole ollama module for them is not worth it.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client talks to a local Ollama server.
type Client struct {
	Host  string
	HTTP  *http.Client
	Stats *Stats
}

// ModelClient is the small provider surface the exam pipeline needs. Ollama,
// Gemini, and DeepSeek implement it, so provider choice does not leak into
// examgen or the prompt adapters.
type ModelClient interface {
	ChatJSON(ctx context.Context, model string, msgs []Message, schema any, opt *Options, out any) error
	Chat(ctx context.Context, model string, msgs []Message, opt *Options) (string, error)
	ChatTools(ctx context.Context, model string, msgs []Message, tools []Tool, opt *Options) (Message, error)
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// New builds a client with a timeout long enough for CPU-assisted generation on
// a laptop, which is the machine this prototype targets.
func New(host string) *Client {
	return &Client{
		Host:  strings.TrimRight(host, "/"),
		HTTP:  &http.Client{Timeout: 10 * time.Minute},
		Stats: NewStats(),
	}
}

// Message is one chat turn. Images carry base64-encoded image data for vision
// models and is omitted for text models.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Images     []string   `json:"images,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is one function invocation the model asked for.
type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

// Tool describes a callable function to the model.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function half of a Tool.
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// Options are the sampling and context knobs. Left as a struct rather than a
// map so a typo is a compile error.
type Options struct {
	NumCtx int `json:"num_ctx,omitempty"`
	// NumPredict bounds the reply. A small model asked for a one-line verdict
	// will narrate its whole reasoning into the field unless stopped; measured,
	// the blind judge averaged 193 output tokens for a verdict needing about 30.
	NumPredict    int     `json:"num_predict,omitempty"`
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p,omitempty"`
	RepeatPenalty float64 `json:"repeat_penalty,omitempty"`
	Seed          int     `json:"seed,omitempty"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Format   any       `json:"format,omitempty"`
	Tools    []Tool    `json:"tools,omitempty"`
	Options  *Options  `json:"options,omitempty"`
	// KeepAlive is a duration string ("10m") or a number of seconds; 0 unloads
	// the model as soon as the response is done.
	KeepAlive any   `json:"keep_alive,omitempty"`
	Think     *bool `json:"think,omitempty"`
}

type chatResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
	Error   string  `json:"error"`

	// All durations are nanoseconds.
	TotalDuration      int64 `json:"total_duration"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalCount          int   `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
}

// ChatJSON runs one non-streaming chat turn constrained to a JSON schema and
// unmarshals the reply into out.
//
// The schema is enforced by the server's grammar, so a malformed reply here
// means something worse than a sloppy model — surface it rather than retry
// silently.
func (c *Client) ChatJSON(ctx context.Context, model string, msgs []Message, schema any, opt *Options, out any) error {
	attempt := func(msgs []Message) (string, error) {
		raw, err := c.chat(ctx, model, msgs, schema, opt)
		if err != nil {
			return "", err
		}
		// Some models still wrap output in a fenced block despite the grammar.
		raw = strings.TrimSpace(raw)
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		return raw, nil
	}

	raw, err := attempt(msgs)
	if err != nil {
		return err
	}
	if json.Unmarshal([]byte(raw), out) == nil {
		return nil
	}

	// One retry. The usual cause is a reply that ran past the context window
	// mid-string, because a small model narrates into a field that was meant to
	// hold a sentence. Asking for brevity fixes it more often than not.
	retry := append(append([]Message{}, msgs...), Message{
		Role:    "user",
		Content: "That reply was cut off before it was valid JSON. Answer again, keeping every text field under 20 words.",
	})
	raw2, err := attempt(retry)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw2), out); err != nil {
		return fmt.Errorf("model returned unparseable JSON twice: %w\n%s", err, excerpt(raw2, 300))
	}
	return nil
}

// Chat runs one plain, unconstrained chat turn.
func (c *Client) Chat(ctx context.Context, model string, msgs []Message, opt *Options) (string, error) {
	return c.chat(ctx, model, msgs, nil, opt)
}

// ChatTools runs one turn with tools available and returns the whole assistant
// message so the caller can see any tool_calls.
//
// Tools and a JSON-schema `format` cannot be combined: the grammar that
// enforces the schema also stops the model from emitting a tool call, and it
// silently answers from its own head instead. Verified against this model —
// with both set it produced schema-valid JSON and never called the tool. Any
// design that needs both has to run them as two separate turns.
func (c *Client) ChatTools(ctx context.Context, model string, msgs []Message, tools []Tool, opt *Options) (Message, error) {
	c.Stats.begin()
	defer c.Stats.end()
	off := false
	req := chatRequest{
		Model:     model,
		Messages:  msgs,
		Stream:    false,
		Tools:     tools,
		Options:   opt,
		KeepAlive: "10m",
		Think:     &off,
	}
	var resp chatResponse
	if err := c.post(ctx, "/api/chat", req, &resp); err != nil {
		return Message{}, err
	}
	c.Stats.add(labelOf(ctx), &resp)
	if resp.Error != "" {
		return Message{}, fmt.Errorf("ollama: %s", resp.Error)
	}
	return resp.Message, nil
}

func (c *Client) chat(ctx context.Context, model string, msgs []Message, schema any, opt *Options) (string, error) {
	c.Stats.begin()
	defer c.Stats.end()
	// Reasoning models burn minutes on a task that has a fixed output shape.
	off := false
	req := chatRequest{
		Model:     model,
		Messages:  msgs,
		Stream:    false,
		Format:    schema,
		Options:   opt,
		KeepAlive: "10m",
		Think:     &off,
	}

	var resp chatResponse
	if err := c.post(ctx, "/api/chat", req, &resp); err != nil {
		return "", err
	}
	c.Stats.add(labelOf(ctx), &resp)
	if resp.Error != "" {
		return "", fmt.Errorf("ollama: %s", resp.Error)
	}
	return resp.Message.Content, nil
}

type embedRequest struct {
	Model     string   `json:"model"`
	Input     []string `json:"input"`
	KeepAlive string   `json:"keep_alive,omitempty"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error"`
}

// Embed returns one vector per input string.
func (c *Client) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	started := c.Stats.begin()
	defer func() {
		if !started.IsZero() {
			c.Stats.addElapsed("embed", time.Since(started))
		}
		c.Stats.end()
	}()
	var resp embedResponse
	if err := c.post(ctx, "/api/embed", embedRequest{Model: model, Input: texts, KeepAlive: "10m"}, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("ollama: %s", resp.Error)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("asked for %d embeddings, got %d", len(texts), len(resp.Embeddings))
	}
	return resp.Embeddings, nil
}

// Unload evicts a model from VRAM immediately. The target machine has 6 GB and
// cannot hold the OCR model and the generation model at the same time.
//
// An empty messages array with keep_alive 0 is the documented way to do this;
// the server answers with done_reason "unload".
func (c *Client) Unload(ctx context.Context, model string) error {
	req := chatRequest{Model: model, Messages: []Message{}, Stream: false, KeepAlive: 0}
	var resp chatResponse
	return c.post(ctx, "/api/chat", req, &resp)
}

// Tags lists the models that have been pulled. /api/tags is used rather than
// /api/show because its behaviour for an unknown model is documented and
// /api/show's is not.
func (c *Client) Tags(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Host+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /api/tags: %w (is ollama running on %s?)", err, c.Host)
	}
	defer resp.Body.Close()

	var tags struct {
		Models []struct {
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Model)
	}
	return names, nil
}

// Installed reports whether a model has been pulled.
func (c *Client) Installed(ctx context.Context, model string) (bool, error) {
	names, err := c.Tags(ctx)
	if err != nil {
		return false, err
	}
	want := model
	if !strings.Contains(want, ":") {
		want += ":latest"
	}
	for _, name := range names {
		if name == want || name == model {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w (is ollama running on %s?)", path, err, c.Host)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %d %s: %s", path, resp.StatusCode, resp.Status, excerpt(buf.String(), 300))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(buf.Bytes(), out); err != nil {
		return fmt.Errorf("POST %s: unparseable response: %w", path, err)
	}
	return nil
}

func excerpt(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
