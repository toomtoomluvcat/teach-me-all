package judging

import (
	"context"
	"fmt"

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
	c     ModelClient
	model string
}

func NewJudge(c ModelClient, model string) *Judge { return &Judge{c: c, model: model} }

func (j *Judge) JudgeBlind(ctx context.Context, q examgen.Question) (examgen.BlindVerdict, error) {
	ctx = WithLabel(ctx, "judge/blind")
	var v examgen.BlindVerdict
	msgs := []Message{
		{Role: "system", Content: examgen.BlindSystem()},
		{Role: "user", Content: examgen.BlindPrompt(q)},
	}
	// This judge never sees the source, so its prompt is a few hundred tokens
	// and 4096 is ample. The cap on output is what matters here: unbounded, the
	// model writes an essay into the reason field and the reply overruns.
	opt := genOptions(4096, 0)
	opt.NumPredict = 400
	err := j.c.ChatJSON(ctx, j.model, msgs, examgen.BlindSchema(len(q.Choices)), opt, &v)
	return v, err
}

func (j *Judge) JudgeAgainstSource(ctx context.Context, q examgen.Question, source string) (examgen.SourcedVerdict, error) {
	ctx = WithLabel(ctx, "judge/source")
	var v examgen.SourcedVerdict
	msgs := []Message{
		{Role: "system", Content: examgen.SourcedSystem()},
		{Role: "user", Content: examgen.SourcedPrompt(q, source)},
	}
	// 8192 covers a chunk plus the question. 12288 was oversized: a larger
	// window costs KV cache on a 6 GB card and buys nothing when the prompt
	// measures a couple of thousand tokens.
	opt := genOptions(8192, 0)
	opt.NumPredict = 220
	err := j.c.ChatJSON(ctx, j.model, msgs, examgen.SourcedSchema(), opt, &v)
	if err != nil {
		return v, err
	}
	if validSourceDependency(v) {
		return v, nil
	}

	// JSON mode guarantees syntax, not schema compliance. Repair only these two
	// fields; asking for the deferred semantic audit here created avoidable
	// contract failures in the live run.
	msgs = append(msgs, Message{Role: "user", Content: "Your reply omitted or malformed the source-dependency fields. Return exactly one JSON object with dependency set to specific, generic, or unclear, and evidence set to one exact passage substring or an empty string. Do not add any other fields."})
	v = examgen.SourcedVerdict{}
	err = j.c.ChatJSON(ctx, j.model, msgs, examgen.SourcedSchema(), opt, &v)
	if err != nil {
		return v, err
	}
	if !validSourceDependency(v) {
		return v, fmt.Errorf("source judge omitted dependency or evidence twice")
	}
	return v, nil
}

func validSourceDependency(v examgen.SourcedVerdict) bool {
	switch v.SourceDependency {
	case examgen.SourceDependencySpecific:
		return len(v.Evidence) > 0
	case examgen.SourceDependencyGeneric, examgen.SourceDependencyUnclear:
		return len(v.Evidence) == 0
	default:
		return false
	}
}

// Embedder adapts the Ollama client to examgen.Embedder.
type Embedder struct {
	c     ModelClient
	model string
}

func NewEmbedder(c ModelClient, model string) *Embedder { return &Embedder{c: c, model: model} }

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.c.Embed(ctx, e.model, texts)
}
