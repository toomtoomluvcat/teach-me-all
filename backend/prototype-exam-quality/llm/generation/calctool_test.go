package generation

import (
	"context"
	"fmt"
	"testing"

	"protoexam/examgen"
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
		facts := gen.cachedFacts(context.Background(), "L01|c1,c2", passage, true)
		if len(facts) != 1 || facts[0].Value != 50 {
			t.Fatalf("candidate %d facts = %#v, want 12.5*4 = 50", i+1, facts)
		}
	}
	if client.loops != 1 {
		t.Fatalf("tool loops = %d, want 1 shared across the three candidates", client.loops)
	}

	// A different evidence packet is a different question, so it pays again.
	if facts := gen.cachedFacts(context.Background(), "L01|c3", passage, true); len(facts) != 1 {
		t.Fatalf("second packet facts = %#v, want one computed fact", facts)
	}
	if client.loops != 2 {
		t.Fatalf("tool loops = %d, want 2 after a new evidence packet", client.loops)
	}
}

func TestFactsKeyIgnoresWhichSlotsFailed(t *testing.T) {
	lesson := examgen.Lesson{ID: "L01", Title: "Newton's Laws"}
	// The context packet is built once per run and handed to every candidate and
	// every bounded repair unchanged; only the contract's slot list shrinks on a
	// repair. Keying on the packet is what makes the repair reuse the arithmetic
	// the first candidate already paid for.
	packet := []examgen.Chunk{{ID: "c2"}, {ID: "c1"}, {ID: "c3"}}
	repairPacket := []examgen.Chunk{{ID: "c1"}, {ID: "c3"}, {ID: "c2"}}

	if factsKey(lesson, packet) != factsKey(lesson, repairPacket) {
		t.Fatalf("repair got a different key:\n %s\n %s", factsKey(lesson, packet), factsKey(lesson, repairPacket))
	}
	if factsKey(lesson, packet) == factsKey(lesson, []examgen.Chunk{{ID: "c1"}}) {
		t.Fatal("a genuinely narrower packet reused arithmetic computed from chunks it was not given")
	}
	if factsKey(lesson, packet) == factsKey(examgen.Lesson{ID: "L02"}, packet) {
		t.Fatal("two lessons collided on one key")
	}
}
