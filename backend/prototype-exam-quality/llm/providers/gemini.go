package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultGeminiHost = "https://generativelanguage.googleapis.com"

// GeminiClient is a small REST client for the Gemini API. It deliberately uses
// the same ModelClient surface as Ollama so the exam pipeline remains provider
// agnostic.
type GeminiClient struct {
	Host   string
	APIKey string
	HTTP   *http.Client
	Stats  *Stats

	// MinInterval serializes requests for low-RPM projects. The CLI sets this
	// to a conservative interval for the observed free tier; tests leave it at
	// zero.
	MinInterval time.Duration
	MaxRetries  int
	rateMu      sync.Mutex
	lastRequest time.Time
}

// NewGeminiAt exists so tests can point the client at an httptest.Server.
func NewGeminiAt(host, apiKey string, httpClient *http.Client) *GeminiClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	return &GeminiClient{
		Host:       strings.TrimRight(host, "/"),
		APIKey:     strings.TrimSpace(apiKey),
		HTTP:       httpClient,
		Stats:      NewStats(),
		MaxRetries: 1,
	}
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiGenerationConfig struct {
	ResponseMIMEType   string   `json:"responseMimeType,omitempty"`
	ResponseJSONSchema any      `json:"responseJsonSchema,omitempty"`
	MaxOutputTokens    int      `json:"maxOutputTokens,omitempty"`
	Temperature        *float64 `json:"temperature,omitempty"`
	TopP               *float64 `json:"topP,omitempty"`
}

type geminiFunctionDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiGenerateRequest struct {
	SystemInstruction *geminiContent          `json:"system_instruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiTool            `json:"tools,omitempty"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	PromptFeedback map[string]any `json:"promptFeedback,omitempty"`
}

func (c *GeminiClient) ChatJSON(ctx context.Context, model string, msgs []Message, schema any, opt *Options, out any) error {
	raw, err := c.ChatJSONRaw(ctx, model, msgs, schema, opt)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), out); err == nil {
		return nil
	}

	retry := append(append([]Message{}, msgs...), Message{
		Role:    "user",
		Content: "That reply was not valid JSON. Answer again and keep every text field under 20 words.",
	})
	raw, err = c.ChatJSONRaw(ctx, model, retry, schema, opt)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("gemini returned unparseable JSON twice: %w\n%s", err, excerpt(raw, 300))
	}
	return nil
}

func (c *GeminiClient) ChatJSONRaw(ctx context.Context, model string, msgs []Message, schema any, opt *Options) (string, error) {
	reply, err := c.generate(ctx, model, msgs, schema, nil, opt)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply.Content), nil
}

func (c *GeminiClient) Chat(ctx context.Context, model string, msgs []Message, opt *Options) (string, error) {
	reply, err := c.generate(ctx, model, msgs, nil, nil, opt)
	if err != nil {
		return "", err
	}
	return reply.Content, nil
}

func (c *GeminiClient) ChatTools(ctx context.Context, model string, msgs []Message, tools []Tool, opt *Options) (Message, error) {
	return c.generate(ctx, model, msgs, nil, tools, opt)
}

func (c *GeminiClient) generate(ctx context.Context, model string, msgs []Message, schema any, tools []Tool, opt *Options) (Message, error) {
	contents, system := geminiContents(msgs)
	req := geminiGenerateRequest{
		SystemInstruction: system,
		Contents:          contents,
		GenerationConfig:  geminiConfig(schema, opt),
		Tools:             geminiTools(tools),
	}

	var resp geminiGenerateResponse
	if err := c.post(ctx, labelOf(ctx), modelPath(model)+":generateContent", req, &resp); err != nil {
		return Message{}, err
	}
	if len(resp.Candidates) == 0 {
		return Message{}, fmt.Errorf("gemini returned no candidates")
	}
	return messageFromGemini(resp.Candidates[0].Content), nil
}

func geminiConfig(schema any, opt *Options) *geminiGenerationConfig {
	cfg := &geminiGenerationConfig{}
	if schema != nil {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseJSONSchema = schema
	}
	if opt == nil {
		return cfg
	}
	if opt.NumPredict > 0 {
		cfg.MaxOutputTokens = opt.NumPredict
	}
	temperature := opt.Temperature
	cfg.Temperature = &temperature
	if opt.TopP > 0 {
		topP := opt.TopP
		cfg.TopP = &topP
	}
	return cfg
}

func geminiContents(msgs []Message) ([]geminiContent, *geminiContent) {
	var contents []geminiContent
	var system strings.Builder
	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(msg.Content)
		case "tool":
			contents = append(contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{{FunctionResponse: &geminiFunctionResponse{
					Name:     msg.ToolName,
					Response: map[string]any{"result": msg.Content},
				}}},
			})
		default:
			content := geminiContent{Role: geminiRole(msg.Role)}
			if msg.Content != "" {
				content.Parts = append(content.Parts, geminiPart{Text: msg.Content})
			}
			for _, call := range msg.ToolCalls {
				content.Parts = append(content.Parts, geminiPart{FunctionCall: &geminiFunctionCall{
					Name: call.Function.Name,
					Args: call.Function.Arguments,
				}})
			}
			if len(content.Parts) > 0 {
				contents = append(contents, content)
			}
		}
	}
	if len(contents) == 0 {
		contents = []geminiContent{{Role: "user", Parts: []geminiPart{{Text: ""}}}}
	}
	if system.Len() == 0 {
		return contents, nil
	}
	return contents, &geminiContent{Parts: []geminiPart{{Text: system.String()}}}
}

func geminiRole(role string) string {
	if role == "assistant" {
		return "model"
	}
	return "user"
}

func geminiTools(tools []Tool) []geminiTool {
	if len(tools) == 0 {
		return nil
	}
	declarations := make([]geminiFunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		declarations = append(declarations, geminiFunctionDeclaration{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return []geminiTool{{FunctionDeclarations: declarations}}
}

func messageFromGemini(content geminiContent) Message {
	var msg Message
	for _, part := range content.Parts {
		if part.Text != "" {
			if msg.Content != "" {
				msg.Content += "\n"
			}
			msg.Content += part.Text
		}
		if part.FunctionCall != nil {
			var call ToolCall
			call.Function.Name = part.FunctionCall.Name
			call.Function.Arguments = part.FunctionCall.Args
			msg.ToolCalls = append(msg.ToolCalls, call)
		}
	}
	return msg
}

type geminiBatchEmbedRequest struct {
	Requests []geminiEmbedRequest `json:"requests"`
}

type geminiEmbedRequest struct {
	Model                string        `json:"model"`
	Content              geminiContent `json:"content"`
	OutputDimensionality int           `json:"outputDimensionality,omitempty"`
}

type geminiBatchEmbedResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}

func (c *GeminiClient) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	requests := make([]geminiEmbedRequest, len(texts))
	for i, text := range texts {
		requests[i] = geminiEmbedRequest{
			Model:   modelResource(model),
			Content: geminiContent{Parts: []geminiPart{{Text: text}}},
			// Match the prototype's bge-m3 dimensionality. The API supports
			// reduced output dimensionality for gemini-embedding-001.
			OutputDimensionality: 1024,
		}
	}
	var resp geminiBatchEmbedResponse
	if err := c.post(ctx, "embed", modelPath(model)+":batchEmbedContents", geminiBatchEmbedRequest{Requests: requests}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("asked for %d Gemini embeddings, got %d", len(texts), len(resp.Embeddings))
	}
	out := make([][]float32, len(resp.Embeddings))
	for i := range resp.Embeddings {
		out[i] = resp.Embeddings[i].Values
	}
	return out, nil
}

func modelResource(model string) string {
	if strings.HasPrefix(model, "models/") {
		return model
	}
	return "models/" + model
}

func modelPath(model string) string {
	return "/v1beta/models/" + url.PathEscape(strings.TrimPrefix(model, "models/"))
}

func (c *GeminiClient) post(ctx context.Context, label, path string, body, out any) error {
	if c.APIKey == "" {
		return fmt.Errorf("GEMINI_API_KEY is empty")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		if err := c.waitForSlot(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+path, bytes.NewReader(b))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", c.APIKey)

		started := c.Stats.Begin()
		resp, err := c.HTTP.Do(req)
		if err != nil {
			c.finishCall(label, started)
			return fmt.Errorf("POST Gemini %s: %w", path, err)
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.finishCall(label, started)
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
				if err := waitContext(ctx, geminiRetryDelay(resp.Header.Get("Retry-After"), string(data), attempt)); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("POST Gemini %s: %d %s: %s", path, resp.StatusCode, resp.Status, excerpt(string(data), 500))
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("POST Gemini %s: unparseable response: %w", path, err)
		}
		return nil
	}
}

func (c *GeminiClient) waitForSlot(ctx context.Context) error {
	if c.MinInterval <= 0 {
		return nil
	}
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if !c.lastRequest.IsZero() {
		if wait := c.MinInterval - time.Since(c.lastRequest); wait > 0 {
			if err := waitContext(ctx, wait); err != nil {
				return err
			}
		}
	}
	c.lastRequest = time.Now()
	return nil
}

func (c *GeminiClient) finishCall(label string, started time.Time) {
	if !started.IsZero() {
		c.Stats.AddElapsed(label, time.Since(started))
	}
	c.Stats.End()
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func geminiRetryDelay(header, body string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	// Gemini often puts the retry hint in the JSON error message rather than a
	// Retry-After header, for example: "Please retry in 35.0s".
	lower := strings.ToLower(body)
	if at := strings.Index(lower, "retry in "); at >= 0 {
		value := lower[at+len("retry in "):]
		if end := strings.IndexByte(value, 's'); end >= 0 {
			if seconds, err := strconv.ParseFloat(strings.TrimSpace(value[:end]), 64); err == nil && seconds >= 0 {
				return time.Duration(seconds * float64(time.Second))
			}
		}
	}
	delay := time.Duration(15*(1<<attempt)) * time.Second
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}
