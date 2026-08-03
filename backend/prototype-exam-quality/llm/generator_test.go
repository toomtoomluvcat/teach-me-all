package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"protoexam/examgen"
)

type repairContractClient struct {
	calls    int
	messages [][]Message
	opts     []*Options
}

type focusedDistractorClient struct {
	calls    int
	messages []Message
}

func (c *focusedDistractorClient) ChatJSON(_ context.Context, _ string, messages []Message, _ any, _ *Options, out any) error {
	c.calls++
	c.messages = messages
	return json.Unmarshal([]byte(`{"replacements":[{"index":1,"content":"wrong order"},{"index":2,"content":"wrong organ"},{"index":3,"content":"wrong process"}]}`), out)
}
func (*focusedDistractorClient) Chat(context.Context, string, []Message, *Options) (string, error) {
	return "", nil
}
func (*focusedDistractorClient) ChatTools(context.Context, string, []Message, []Tool, *Options) (Message, error) {
	return Message{}, nil
}
func (*focusedDistractorClient) Embed(context.Context, string, []string) ([][]float32, error) {
	return nil, nil
}

func (c *repairContractClient) ChatJSON(_ context.Context, _ string, messages []Message, _ any, opt *Options, out any) error {
	c.calls++
	c.messages = append(c.messages, messages)
	c.opts = append(c.opts, opt)
	response := out.(*repairResponse)
	choice := "equivalent wording"
	if c.calls == 2 {
		choice = "materially false wording"
	}
	response.Questions = []examgen.Question{{
		Kind: examgen.KindMCQSingle, Stem: "Which answer is supported?", SourceQuote: "a sufficiently long source quote for testing",
		Choices: []examgen.Choice{{Content: "correct", IsCorrect: true}, {Content: choice}, {Content: "wrong C"}, {Content: "wrong D"}},
	}}
	return nil
}

func (*repairContractClient) Chat(context.Context, string, []Message, *Options) (string, error) {
	return "", nil
}
func (*repairContractClient) ChatTools(context.Context, string, []Message, []Tool, *Options) (Message, error) {
	return Message{}, nil
}
func (*repairContractClient) Embed(context.Context, string, []string) ([][]float32, error) {
	return nil, nil
}

func TestRepairResponseAcceptsHostedSingleQuestionShapes(t *testing.T) {
	question := `{"kind":"mcq_single","stem":"Which answer is supported?","choices":[{"content":"A","is_correct":true},{"content":"B","is_correct":false}],"explanation":"A","source_quote":"a sufficiently long source quote for testing","difficulty":"easy","skill":"recall"}`
	for name, payload := range map[string]string{
		"array":    `{"questions":[` + question + `]}`,
		"singular": `{"question":` + question + `}`,
		"direct":   question,
	} {
		t.Run(name, func(t *testing.T) {
			var response repairResponse
			if err := json.Unmarshal([]byte(payload), &response); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			q, ok := response.first()
			if !ok || q.Stem != "Which answer is supported?" {
				t.Fatalf("first() = %#v, %v", q, ok)
			}
		})
	}
}

func TestGeneratorRepairRetriesWhenMandatoryChoiceWasNotChanged(t *testing.T) {
	client := &repairContractClient{}
	gen := NewGenerator(client, "hosted-model")
	q := examgen.Question{
		Kind: examgen.KindMCQSingle, Stem: "Which answer is supported?", SourceQuote: "a sufficiently long source quote for testing",
		Choices: []examgen.Choice{{Content: "correct", IsCorrect: true}, {Content: "equivalent wording"}, {Content: "wrong C"}, {Content: "wrong D"}},
	}
	failure := examgen.GateResult{Gate: examgen.GateBlindAnswer, ChoiceVerdicts: []examgen.ChoiceVerdict{
		{Index: 0, Status: examgen.ChoiceSupported, Reason: "supported"},
		{Index: 1, Status: examgen.ChoiceEquivalent, Reason: "same meaning"},
		{Index: 2, Status: examgen.ChoiceUnsupported, Reason: "wrong"},
		{Index: 3, Status: examgen.ChoiceUnsupported, Reason: "wrong"},
	}}

	fixed, ok, err := gen.Repair(context.Background(), q, examgen.Chunk{Page: 1, Text: q.SourceQuote}, []examgen.GateResult{failure}, false)
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !ok || fixed.Choices[1].Content != "materially false wording" {
		t.Fatalf("Repair() = %#v, %v", fixed, ok)
	}
	if client.calls != 2 {
		t.Fatalf("provider calls = %d, want one contract retry", client.calls)
	}
	if client.opts[1].Temperature <= client.opts[0].Temperature {
		t.Fatalf("contract retry temperature = %g, want above deterministic first attempt %g", client.opts[1].Temperature, client.opts[0].Temperature)
	}
	last := client.messages[1][len(client.messages[1])-1].Content
	if !strings.Contains(last, "choice 2") || !strings.Contains(last, "unchanged") {
		t.Fatalf("contract retry feedback = %q", last)
	}
}

func TestGeneratorRepairUsesFocusedDistractorContractForSemanticAudit(t *testing.T) {
	client := &focusedDistractorClient{}
	gen := NewGenerator(client, "hosted-model")
	q := examgen.Question{
		Kind: examgen.KindMCQSingle, Stem: "Which answer is supported?", SourceQuote: "a sufficiently long source quote for testing",
		Choices: []examgen.Choice{{Content: "correct", IsCorrect: true}, {Content: "equivalent"}, {Content: "old C"}, {Content: "old D"}},
	}
	failure := examgen.GateResult{Gate: examgen.GateSingleValid, ChoiceVerdicts: []examgen.ChoiceVerdict{
		{Index: 0, Status: examgen.ChoiceSupported, Reason: "supported"},
		{Index: 1, Status: examgen.ChoiceEquivalent, Reason: "same meaning"},
		{Index: 2, Status: examgen.ChoiceUnsupported, Reason: "wrong"},
		{Index: 3, Status: examgen.ChoiceUnsupported, Reason: "wrong"},
	}}

	fixed, ok, err := gen.Repair(context.Background(), q, examgen.Chunk{Page: 1, Text: q.SourceQuote}, []examgen.GateResult{failure}, false)
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !ok || fixed.Stem != q.Stem || fixed.Choices[0] != q.Choices[0] || fixed.Choices[1].Content != "wrong order" {
		t.Fatalf("focused Repair() = %#v, %v", fixed, ok)
	}
	if client.calls != 1 || !strings.Contains(client.messages[0].Content, "repair only the distractors") {
		t.Fatalf("focused repair calls/system = %d/%q", client.calls, client.messages[0].Content)
	}
}

func TestApplyDistractorReplacementsAcceptsCompleteOneBasedIndices(t *testing.T) {
	q := examgen.Question{Choices: []examgen.Choice{
		{Content: "correct", IsCorrect: true}, {Content: "old B"}, {Content: "old C"}, {Content: "old D"},
	}}
	fixed, ok := applyDistractorReplacements(q, []distractorReplacement{
		{Index: 2, Content: "new B"}, {Index: 3, Content: "new C"}, {Index: 4, Content: "new D"},
	})
	if !ok || fixed.Choices[1].Content != "new B" || fixed.Choices[3].Content != "new D" {
		t.Fatalf("applyDistractorReplacements() = %#v, %v", fixed, ok)
	}
}
