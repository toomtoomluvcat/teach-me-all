package generation

import (
	"context"
	"fmt"
	"testing"
)

// countingToolClient answers the calculator loop with one tool call on the
// first turn and DONE on the second, and counts how many times the loop ran.
type countingToolClient struct {
	loops int
	turns int
}

func (c *countingToolClient) ChatTools(_ context.Context, _ string, msgs []Message, _ []Tool, _ *Options) (Message, error) {
	c.turns++
	// The loop always opens with system + user; a longer conversation means we
	// are inside the same loop rather than starting a new one.
	if len(msgs) == 2 {
		c.loops++
		reply := Message{Role: "assistant"}
		call := ToolCall{ID: "1"}
		call.Function.Name = "calc"
		call.Function.Arguments = map[string]any{"expression": "12.5*4"}
		reply.ToolCalls = []ToolCall{call}
		return reply, nil
	}
	return Message{Role: "assistant", Content: "DONE"}, nil
}

func (c *countingToolClient) ChatJSON(context.Context, string, []Message, any, *Options, any) error {
	return fmt.Errorf("not used")
}
func (c *countingToolClient) Chat(context.Context, string, []Message, *Options) (string, error) {
	return "", fmt.Errorf("not used")
}
func (c *countingToolClient) Embed(context.Context, string, []string) ([][]float32, error) {
	return nil, fmt.Errorf("not used")
}

func TestCachedFactsRunsTheToolLoopOncePerChunkSet(t *testing.T) {
	client := &countingToolClient{}
	gen := NewGenerator(client, "test-model")
	passage := "The sample mass is 12.5 g and there are 4 replicates."

	for i := 0; i < 3; i++ {
		facts := gen.cachedFacts(context.Background(), "true|c1,c2", passage, true)
		if len(facts) != 1 || facts[0].Value != 50 {
			t.Fatalf("candidate %d facts = %#v, want 12.5*4 = 50", i+1, facts)
		}
	}
	if client.loops != 1 {
		t.Fatalf("tool loops = %d, want 1 shared across the three candidates", client.loops)
	}

	// A different slot chunk set is a different question, so it pays again.
	if facts := gen.cachedFacts(context.Background(), "true|c3", passage, true); len(facts) != 1 {
		t.Fatalf("second chunk set facts = %#v, want one computed fact", facts)
	}
	if client.loops != 2 {
		t.Fatalf("tool loops = %d, want 2 after a new chunk set", client.loops)
	}
}
