package llm

import (
	"context"
	"testing"

	"protoexam/examgen"
)

type retryingJudgeClient struct {
	calls int
}

func (c *retryingJudgeClient) ChatJSON(_ context.Context, _ string, _ []Message, _ any, _ *Options, out any) error {
	c.calls++
	verdict := out.(*examgen.SourcedVerdict)
	verdict.BestIndex = 0
	if c.calls == 1 {
		return nil
	}
	verdict.ChoiceVerdicts = []examgen.ChoiceVerdict{
		{Index: 0, Status: examgen.ChoiceSupported, Reason: "supported"},
		{Index: 1, Status: examgen.ChoiceUnsupported, Reason: "unsupported"},
		{Index: 2, Status: examgen.ChoiceEquivalent, Reason: "same meaning"},
		{Index: 3, Status: examgen.ChoiceUnsupported, Reason: "unsupported"},
	}
	return nil
}

func (*retryingJudgeClient) Chat(context.Context, string, []Message, *Options) (string, error) {
	return "", nil
}

func (*retryingJudgeClient) ChatTools(context.Context, string, []Message, []Tool, *Options) (Message, error) {
	return Message{}, nil
}

func (*retryingJudgeClient) Embed(context.Context, string, []string) ([][]float32, error) {
	return nil, nil
}

func TestJudgeAgainstSourceRetriesWhenPerChoiceAuditIsMissing(t *testing.T) {
	client := &retryingJudgeClient{}
	judge := NewJudge(client, "test-model")
	q := examgen.Question{Choices: []examgen.Choice{
		{Content: "correct", IsCorrect: true},
		{Content: "wrong"},
		{Content: "same meaning"},
		{Content: "wrong"},
	}}

	verdict, err := judge.JudgeAgainstSource(context.Background(), q, "source")
	if err != nil {
		t.Fatalf("JudgeAgainstSource() error = %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("provider calls = %d, want one contract-repair retry", client.calls)
	}
	if len(verdict.ChoiceVerdicts) != len(q.Choices) {
		t.Fatalf("choice verdicts = %#v", verdict.ChoiceVerdicts)
	}
}
