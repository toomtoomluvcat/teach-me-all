package llm

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
	opt.NumPredict = 900
	err := j.c.ChatJSON(ctx, j.model, msgs, examgen.SourcedSchema(len(q.Choices)), opt, &v)
	if err != nil {
		return v, err
	}
	if validChoiceAudit(v.ChoiceVerdicts, len(q.Choices)) {
		deriveSourcedSummary(&v)
		return v, nil
	}

	// DeepSeek JSON mode guarantees syntax, not schema compliance. A valid
	// object that silently omits choice_verdicts must not pass the question.
	msgs = append(msgs, Message{Role: "user", Content: fmt.Sprintf(
		"Your reply omitted or malformed choice_verdicts. Return the entire JSON object again with exactly %d choice_verdicts, one unique index for every choice from 0 through %d. Audit synonyms, paraphrases, broader/narrower wording, and ordinary-language equivalents explicitly.",
		len(q.Choices), len(q.Choices)-1,
	)})
	v = examgen.SourcedVerdict{}
	err = j.c.ChatJSON(ctx, j.model, msgs, examgen.SourcedSchema(len(q.Choices)), opt, &v)
	if err != nil {
		return v, err
	}
	if !validChoiceAudit(v.ChoiceVerdicts, len(q.Choices)) {
		return v, fmt.Errorf("source judge omitted a complete per-choice semantic audit twice")
	}
	deriveSourcedSummary(&v)
	return v, err
}

func deriveSourcedSummary(v *examgen.SourcedVerdict) {
	v.BestIndex = -1
	v.AlsoDefensible = nil
	for _, verdict := range v.ChoiceVerdicts {
		switch verdict.Status {
		case examgen.ChoiceSupported:
			if v.BestIndex < 0 {
				v.BestIndex = verdict.Index
			} else {
				v.AlsoDefensible = append(v.AlsoDefensible, verdict.Index)
			}
		case examgen.ChoiceEquivalent, examgen.ChoiceAmbiguous:
			v.AlsoDefensible = append(v.AlsoDefensible, verdict.Index)
		}
	}
	v.Reason = "derived from the per-choice semantic audit"
}

func validChoiceAudit(verdicts []examgen.ChoiceVerdict, choices int) bool {
	if len(verdicts) != choices {
		return false
	}
	seen := make([]bool, choices)
	for _, verdict := range verdicts {
		if verdict.Index < 0 || verdict.Index >= choices || seen[verdict.Index] || verdict.Reason == "" {
			return false
		}
		switch verdict.Status {
		case examgen.ChoiceSupported, examgen.ChoiceUnsupported, examgen.ChoiceEquivalent, examgen.ChoiceAmbiguous:
		default:
			return false
		}
		seen[verdict.Index] = true
	}
	return true
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
