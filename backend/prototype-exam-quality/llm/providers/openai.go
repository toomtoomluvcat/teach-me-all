package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient talks to any host that speaks the OpenAI /chat/completions
// wire format. That is deliberately most of the market: DeepSeek, OpenRouter,
// Groq, Together, Mistral, Fireworks, and — for a local model — vLLM,
// llama.cpp, LM Studio, and Ollama's /v1 endpoint all accept these requests,
// so the base URL is the only thing that changes between them.
//
// JSON mode here validates JSON syntax and nothing more; the pipeline's Go
// gates remain the authority on question shape and semantics.
type OpenAIClient struct {
	Host   string
	APIKey string
	HTTP   *http.Client
	Stats  *Stats
}

// NewOpenAICompatibleAt points the client at one base URL. The URL is the
// origin plus any version prefix the host uses ("https://api.deepseek.com",
// "http://localhost:11434/v1"), without the /chat/completions suffix.
func NewOpenAICompatibleAt(host, apiKey string, httpClient *http.Client) *OpenAIClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	return &OpenAIClient{
		Host:   strings.TrimRight(host, "/"),
		APIKey: strings.TrimSpace(apiKey),
		HTTP:   httpClient,
		Stats:  NewStats(),
	}
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []wireMessage       `json:"messages"`
	Stream         bool                `json:"stream"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	Temperature    *float64            `json:"temperature,omitempty"`
	TopP           *float64            `json:"top_p,omitempty"`
	Tools          []wireTool          `json:"tools,omitempty"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type"`
	Function wireToolCallFunction `json:"function"`
}

type wireToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

type wireToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []wireToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *OpenAIClient) ChatJSON(ctx context.Context, model string, msgs []Message, schema any, opt *Options, out any) error {
	raw, err := c.ChatJSONRaw(ctx, model, msgs, opt)
	if err != nil {
		return err
	}
	if json.Unmarshal([]byte(cleanJSONReply(raw)), out) == nil {
		return nil
	}

	retry := append(append([]Message{}, msgs...), Message{
		Role:    "user",
		Content: "The previous reply was JSON but did not match the requested shape. Return one JSON object only, with exactly the requested field names and nested object types; do not use legacy field names, arrays of strings where objects were requested, or markdown fences.",
	})
	raw, err = c.ChatJSONRaw(ctx, model, retry, opt)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(cleanJSONReply(raw)), out); err != nil {
		return fmt.Errorf("provider returned unparseable JSON twice: %w\n%s", err, excerpt(raw, 300))
	}
	return nil
}

func (c *OpenAIClient) ChatJSONRaw(ctx context.Context, model string, msgs []Message, opt *Options) (string, error) {
	reply, err := c.generate(ctx, model, msgs, true, nil, opt)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply.Content), nil
}

func (c *OpenAIClient) Chat(ctx context.Context, model string, msgs []Message, opt *Options) (string, error) {
	reply, err := c.generate(ctx, model, msgs, false, nil, opt)
	if err != nil {
		return "", err
	}
	return reply.Content, nil
}

func (c *OpenAIClient) ChatTools(ctx context.Context, model string, msgs []Message, tools []Tool, opt *Options) (Message, error) {
	return c.generate(ctx, model, msgs, false, tools, opt)
}

func (c *OpenAIClient) generate(ctx context.Context, model string, msgs []Message, jsonMode bool, tools []Tool, opt *Options) (Message, error) {
	req := chatRequest{
		Model:    model,
		Messages: wireMessages(msgs, jsonMode),
		Stream:   false,
		Tools:    wireTools(tools),
	}
	if jsonMode {
		req.ResponseFormat = &chatResponseFormat{Type: "json_object"}
	}
	if opt != nil {
		if opt.NumPredict > 0 {
			req.MaxTokens = opt.NumPredict
		}
		temperature := opt.Temperature
		req.Temperature = &temperature
		if opt.TopP > 0 {
			topP := opt.TopP
			req.TopP = &topP
		}
	}

	var resp chatResponse
	if err := c.post(ctx, labelOf(ctx), req, &resp); err != nil {
		return Message{}, err
	}
	c.Stats.AddTokens(labelOf(ctx), resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	if resp.Error != nil && resp.Error.Message != "" {
		return Message{}, fmt.Errorf("provider error: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return Message{}, fmt.Errorf("provider returned no choices")
	}
	return messageFromWire(resp.Choices[0].Message)
}

func wireMessages(msgs []Message, jsonMode bool) []wireMessage {
	out := make([]wireMessage, 0, len(msgs)+1)
	hasJSONInstruction := false
	for _, msg := range msgs {
		if strings.Contains(strings.ToLower(msg.Content), "json") {
			hasJSONInstruction = true
		}
		converted := wireMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		}
		for _, call := range msg.ToolCalls {
			args, err := json.Marshal(call.Function.Arguments)
			if err != nil {
				args = []byte(`{}`)
			}
			converted.ToolCalls = append(converted.ToolCalls, wireToolCall{
				ID:   call.ID,
				Type: "function",
				Function: wireToolCallFunction{
					Name:      call.Function.Name,
					Arguments: string(args),
				},
			})
		}
		out = append(out, converted)
	}
	if jsonMode && !hasJSONInstruction {
		out = append([]wireMessage{{Role: "system", Content: "Return valid JSON only."}}, out...)
	}
	return out
}

func wireTools(tools []Tool) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, wireTool{
			Type: "function",
			Function: wireToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
	}
	return out
}

func messageFromWire(message struct {
	Content   string         `json:"content"`
	ToolCalls []wireToolCall `json:"tool_calls"`
}) (Message, error) {
	out := Message{Role: "assistant", Content: message.Content}
	for _, call := range message.ToolCalls {
		var args map[string]any
		if strings.TrimSpace(call.Function.Arguments) == "" {
			args = map[string]any{}
		} else if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return Message{}, fmt.Errorf("DeepSeek returned invalid tool arguments: %w", err)
		}
		var tc ToolCall
		tc.ID = call.ID
		tc.Function.Name = call.Function.Name
		tc.Function.Arguments = args
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	return out, nil
}

func cleanJSONReply(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

// Embed calls the OpenAI-compatible /embeddings endpoint. Hosted chat-only
// providers answer with an error, which is the honest outcome: the caller then
// leaves --embed-model empty and the duplicate gate falls back to its
// non-vector checks. Local servers and most aggregators do implement it.
func (c *OpenAIClient) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	var resp embeddingResponse
	if err := c.postTo(ctx, "/embeddings", "embed", embeddingRequest{Model: model, Input: texts}, &resp); err != nil {
		return nil, err
	}
	if resp.Error.Message != "" {
		return nil, fmt.Errorf("provider error: %s", resp.Error.Message)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: got %d vectors for %d inputs", len(resp.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for _, item := range resp.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("embeddings: out-of-range index %d", item.Index)
		}
		vec := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			vec[i] = float32(v)
		}
		out[item.Index] = vec
	}
	for i, vec := range out {
		if vec == nil {
			return nil, fmt.Errorf("embeddings: no vector for input %d", i)
		}
	}
	return out, nil
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenAIClient) post(ctx context.Context, label string, body, out any) error {
	return c.postTo(ctx, "/chat/completions", label, body, out)
}

// postTo is the single HTTP path. A local server behind an OpenAI-compatible
// facade often needs no key at all, so an empty key is only an error when the
// host asks for one — which it reports as a 401 with its own message, and that
// is more useful than a guess made here.
func (c *OpenAIClient) postTo(ctx context.Context, path, label string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	started := c.Stats.Begin()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.finishCall(label, started)
		return fmt.Errorf("POST %s%s: %w", c.Host, path, err)
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	c.finishCall(label, started)
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s%s: %d %s: %s", c.Host, path, resp.StatusCode, resp.Status, excerpt(string(data), 500))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("POST %s%s: unparseable response: %w", c.Host, path, err)
	}
	return nil
}

func (c *OpenAIClient) finishCall(label string, started time.Time) {
	if !started.IsZero() {
		c.Stats.AddElapsed(label, time.Since(started))
	}
	c.Stats.End()
}

var _ ModelClient = (*OpenAIClient)(nil)
