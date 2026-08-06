package judging

import (
	"context"
)

// Embedder adapts a provider's embedding endpoint to examgen.Embedder. It is
// the only model call left in this package that is not the advisory quality
// grader: stem vectors feed the deterministic duplicate gate.
type Embedder struct {
	c     ModelClient
	model string
}

func NewEmbedder(c ModelClient, model string) *Embedder { return &Embedder{c: c, model: model} }

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.c.Embed(ctx, e.model, texts)
}
