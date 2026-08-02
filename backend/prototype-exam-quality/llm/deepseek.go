package llm

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

const defaultDeepSeekHost = "https://api.deepseek.com"

// DeepSeekClient uses DeepSeek's OpenAI-compatible /chat/completions API. Its
// JSON mode validates the JSON syntax, while the pipeline's Go gates validate
// the actual question shape and semantics.
type DeepSeekClient struct {
	Host   string
	APIKey string
	HTTP   *http.Client
	Stats  *Stats
}

func NewDeepSeek(apiKey string) *DeepSeekClient {
	return NewDeepSeekAt(defaultDeepSeekHost, apiKey, nil)
}

func NewDeepSeekAt(host, apiKey string, httpClient *http.Client) *DeepSeekClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	return &DeepSeekClient{
		Host:   strings.TrimRight(host, "/"),
		APIKey: strings.TrimSpace(apiKey),
		HTTP:   httpClient,
		Stats:  NewStats(),
	}
}

type deepSeekRequest struct {
	Model          string                  `json:"model"`
	Messages       []deepSeekMessage       `json:"messages"`
	Stream         bool                    `json:"stream"`
	ResponseFormat *deepSeekResponseFormat `json:"response_format,omitempty"`
	MaxTokens      int                     `json:"max_tokens,omitempty"`
	Temperature    *float64                `json:"temperature,omitempty"`
	TopP           *float64                `json:"top_p,omitempty"`
	Tools          []deepSeekTool          `json:"tools,omitempty"`
}

type deepSeekResponseFormat struct {
	Type string `json:"type"`
}

type deepSeekMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content,omitempty"`
	ToolCalls  []deepSeekToolCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

type deepSeekToolCall struct {
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type"`
	Function deepSeekToolCallFunction `json:"function"`
}

type deepSeekToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type deepSeekTool struct {
	Type     string               `json:"type"`
	Function deepSeekToolFunction `json:"function"`
}

type deepSeekToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type deepSeekResponse struct {
	Choices []struct {
		Message struct {
			Content   string             `json:"content"`
			ToolCalls []deepSeekToolCall `json:"tool_calls"`
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

func (c *DeepSeekClient) ChatJSON(ctx context.Context, model string, msgs []Message, schema any, opt *Options, out any) error {
	raw, err := c.ChatJSONRaw(ctx, model, msgs, opt)
	if err != nil {
		return err
	}
	if json.Unmarshal([]byte(cleanDeepSeekJSON(raw)), out) == nil {
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
	if err := json.Unmarshal([]byte(cleanDeepSeekJSON(raw)), out); err != nil {
		return fmt.Errorf("DeepSeek returned unparseable JSON twice: %w\n%s", err, excerpt(raw, 300))
	}
	return nil
}

func (c *DeepSeekClient) ChatJSONRaw(ctx context.Context, model string, msgs []Message, opt *Options) (string, error) {
	reply, err := c.generate(ctx, model, msgs, true, nil, opt)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply.Content), nil
}

func (c *DeepSeekClient) Chat(ctx context.Context, model string, msgs []Message, opt *Options) (string, error) {
	reply, err := c.generate(ctx, model, msgs, false, nil, opt)
	if err != nil {
		return "", err
	}
	return reply.Content, nil
}

func (c *DeepSeekClient) ChatTools(ctx context.Context, model string, msgs []Message, tools []Tool, opt *Options) (Message, error) {
	return c.generate(ctx, model, msgs, false, tools, opt)
}

func (c *DeepSeekClient) generate(ctx context.Context, model string, msgs []Message, jsonMode bool, tools []Tool, opt *Options) (Message, error) {
	req := deepSeekRequest{
		Model:    model,
		Messages: deepSeekMessages(msgs, jsonMode),
		Stream:   false,
		Tools:    deepSeekTools(tools),
	}
	if jsonMode {
		req.ResponseFormat = &deepSeekResponseFormat{Type: "json_object"}
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

	var resp deepSeekResponse
	if err := c.post(ctx, labelOf(ctx), req, &resp); err != nil {
		return Message{}, err
	}
	c.Stats.addTokens(labelOf(ctx), resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	if resp.Error != nil && resp.Error.Message != "" {
		return Message{}, fmt.Errorf("DeepSeek: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return Message{}, fmt.Errorf("DeepSeek returned no choices")
	}
	return messageFromDeepSeek(resp.Choices[0].Message)
}

func deepSeekMessages(msgs []Message, jsonMode bool) []deepSeekMessage {
	out := make([]deepSeekMessage, 0, len(msgs)+1)
	hasJSONInstruction := false
	for _, msg := range msgs {
		if strings.Contains(strings.ToLower(msg.Content), "json") {
			hasJSONInstruction = true
		}
		converted := deepSeekMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		}
		for _, call := range msg.ToolCalls {
			args, err := json.Marshal(call.Function.Arguments)
			if err != nil {
				args = []byte(`{}`)
			}
			converted.ToolCalls = append(converted.ToolCalls, deepSeekToolCall{
				ID:   call.ID,
				Type: "function",
				Function: deepSeekToolCallFunction{
					Name:      call.Function.Name,
					Arguments: string(args),
				},
			})
		}
		out = append(out, converted)
	}
	if jsonMode && !hasJSONInstruction {
		out = append([]deepSeekMessage{{Role: "system", Content: "Return valid JSON only."}}, out...)
	}
	return out
}

func deepSeekTools(tools []Tool) []deepSeekTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]deepSeekTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, deepSeekTool{
			Type: "function",
			Function: deepSeekToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
	}
	return out
}

func messageFromDeepSeek(message struct {
	Content   string             `json:"content"`
	ToolCalls []deepSeekToolCall `json:"tool_calls"`
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

func cleanDeepSeekJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

func (c *DeepSeekClient) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("DeepSeek API has no embeddings endpoint; leave --embed-model empty")
}

func (c *DeepSeekClient) post(ctx context.Context, label string, body, out any) error {
	if c.APIKey == "" {
		return fmt.Errorf("DEEPSEEK_API_KEY is empty")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	started := c.Stats.begin()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.finishCall(label, started)
		return fmt.Errorf("POST DeepSeek /chat/completions: %w", err)
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	c.finishCall(label, started)
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST DeepSeek /chat/completions: %d %s: %s", resp.StatusCode, resp.Status, excerpt(string(data), 500))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("POST DeepSeek /chat/completions: unparseable response: %w", err)
	}
	return nil
}

func (c *DeepSeekClient) finishCall(label string, started time.Time) {
	if !started.IsZero() {
		c.Stats.addElapsed(label, time.Since(started))
	}
	c.Stats.end()
}

var _ ModelClient = (*DeepSeekClient)(nil)
