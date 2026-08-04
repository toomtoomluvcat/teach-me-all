package generation

import (
	"context"
	"net/http"

	"protoexam/llm/core"
	"protoexam/llm/providers"
)

type ModelClient = core.ModelClient
type Message = core.Message
type Tool = core.Tool
type ToolCall = core.ToolCall
type ToolFunction = core.ToolFunction
type Options = core.Options

func WithLabel(ctx context.Context, label string) context.Context { return core.WithLabel(ctx, label) }

func NewDeepSeekAt(host, apiKey string, client *http.Client) *providers.DeepSeekClient {
	return providers.NewDeepSeekAt(host, apiKey, client)
}

func NewGeminiAt(host, apiKey string, client *http.Client) *providers.GeminiClient {
	return providers.NewGeminiAt(host, apiKey, client)
}
