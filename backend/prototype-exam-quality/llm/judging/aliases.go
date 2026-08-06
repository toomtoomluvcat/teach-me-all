package judging

import (
	"context"

	"protoexam/llm/core"
)

type ModelClient = core.ModelClient
type Message = core.Message
type Options = core.Options
type Tool = core.Tool
type ToolFunction = core.ToolFunction

func WithLabel(ctx context.Context, label string) context.Context { return core.WithLabel(ctx, label) }

func genOptions(numCtx int, temp float64) *Options {
	return &Options{NumCtx: numCtx, Temperature: temp, TopP: 0.9, RepeatPenalty: 1.1, Seed: 1}
}
