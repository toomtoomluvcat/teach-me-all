// Package llm is the provider-neutral facade used by the prototype app.
// Concrete clients and adapters live in the folders below this package.
package llm

import (
	"context"
	"net/http"

	"protoexam/examgen"
	"protoexam/llm/core"
	"protoexam/llm/generation"
	"protoexam/llm/judging"
	"protoexam/llm/providers"
)

type Client = core.Client
type ModelClient = core.ModelClient
type Message = core.Message
type ToolCall = core.ToolCall
type Tool = core.Tool
type ToolFunction = core.ToolFunction
type Options = core.Options
type Stats = core.Stats
type Bucket = core.Bucket

type DeepSeekClient = providers.DeepSeekClient
type GeminiClient = providers.GeminiClient
type Generator = generation.Generator
type BatchedTopicGenerator = generation.BatchedTopicGenerator
type Judge = judging.Judge
type Embedder = judging.Embedder
type QualityGrader = judging.QualityGrader

func New(host string) *Client                                     { return core.New(host) }
func NewStats() *Stats                                            { return core.NewStats() }
func WithLabel(ctx context.Context, label string) context.Context { return core.WithLabel(ctx, label) }

func NewDeepSeek(apiKey string) *DeepSeekClient { return providers.NewDeepSeek(apiKey) }
func NewDeepSeekAt(host, apiKey string, client *http.Client) *DeepSeekClient {
	return providers.NewDeepSeekAt(host, apiKey, client)
}
func NewGemini(apiKey string) *GeminiClient { return providers.NewGemini(apiKey) }
func NewGeminiAt(host, apiKey string, client *http.Client) *GeminiClient {
	return providers.NewGeminiAt(host, apiKey, client)
}

func NewGenerator(c ModelClient, model string) *Generator {
	return generation.NewGenerator(c, model)
}
func NewBatchedTopicGenerator(c ModelClient, model string, parallel ...int) *BatchedTopicGenerator {
	return generation.NewBatchedTopicGenerator(c, model, parallel...)
}
func PlannedTopicBatches(chunks []examgen.Chunk) int    { return generation.PlannedTopicBatches(chunks) }
func NewJudge(c ModelClient, model string) *Judge       { return judging.NewJudge(c, model) }
func NewEmbedder(c ModelClient, model string) *Embedder { return judging.NewEmbedder(c, model) }
func NewQualityGrader(c ModelClient, model string) *QualityGrader {
	return judging.NewQualityGrader(c, model)
}
