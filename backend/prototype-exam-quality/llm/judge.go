package llm

import (
	"context"

	"protoexam/examgen"
)

// Judge adapts the Ollama client to examgen.Judge.
//
// It runs on the same model as the generator in this prototype. That is a known
// weakness — a model is a soft grader of its own output — but a second model
// does not fit in 6 GB alongside the first, and swapping models per question
// would make a run take hours. Gates 1 and 4 are deterministic precisely
// because gates 2 and 3 have this problem.
type Judge struct {
	c     *Client
	model string
}

func NewJudge(c *Client, model string) *Judge { return &Judge{c: c, model: model} }

func (j *Judge) JudgeBlind(ctx context.Context, q examgen.Question) (examgen.BlindVerdict, error) {
	var v examgen.BlindVerdict
	msgs := []Message{
		{Role: "system", Content: examgen.BlindSystem()},
		{Role: "user", Content: examgen.BlindPrompt(q)},
	}
	// 8192 rather than 4096: a 4B model narrates its reasoning into the reason
	// field even when told not to, and a truncated reply is unparseable JSON
	// rather than a bad verdict — the run dies instead of the question failing.
	err := j.c.ChatJSON(ctx, j.model, msgs, examgen.BlindSchema(len(q.Choices)), genOptions(8192, 0), &v)
	return v, err
}

func (j *Judge) JudgeAgainstSource(ctx context.Context, q examgen.Question, source string) (examgen.SourcedVerdict, error) {
	var v examgen.SourcedVerdict
	msgs := []Message{
		{Role: "system", Content: examgen.SourcedSystem()},
		{Role: "user", Content: examgen.SourcedPrompt(q, source)},
	}
	err := j.c.ChatJSON(ctx, j.model, msgs, examgen.SourcedSchema(len(q.Choices)), genOptions(12288, 0), &v)
	return v, err
}

// Embedder adapts the Ollama client to examgen.Embedder.
type Embedder struct {
	c     *Client
	model string
}

func NewEmbedder(c *Client, model string) *Embedder { return &Embedder{c: c, model: model} }

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.c.Embed(ctx, e.model, texts)
}
