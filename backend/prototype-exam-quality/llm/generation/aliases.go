package generation

import (
	"context"

	"protoexam/llm/core"
)

type ModelClient = core.ModelClient
type Message = core.Message
type Tool = core.Tool
type ToolCall = core.ToolCall
type ToolFunction = core.ToolFunction
type Options = core.Options

func WithLabel(ctx context.Context, label string) context.Context { return core.WithLabel(ctx, label) }
