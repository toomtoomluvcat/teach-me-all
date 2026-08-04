package providers

import (
	"context"
	"strings"

	"protoexam/llm/core"
)

type Stats = core.Stats
type Bucket = core.Bucket
type Message = core.Message
type ToolCall = core.ToolCall
type Tool = core.Tool
type ToolFunction = core.ToolFunction
type Options = core.Options
type ModelClient = core.ModelClient

func NewStats() *Stats                                            { return core.NewStats() }
func WithLabel(ctx context.Context, label string) context.Context { return core.WithLabel(ctx, label) }
func labelOf(ctx context.Context) string                          { return core.LabelOf(ctx) }

func excerpt(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}
