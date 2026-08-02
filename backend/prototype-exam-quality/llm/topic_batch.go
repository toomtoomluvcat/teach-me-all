package llm

import (
	"context"
	"fmt"

	"protoexam/examgen"
)

// BatchedTopicGenerator uses a provider's larger context window to map all
// chunks in one structured request. The normal Generator.Topics path remains
// unchanged for Ollama and for any caller that does not opt into this feature.
type BatchedTopicGenerator struct {
	c     ModelClient
	model string
}

func NewBatchedTopicGenerator(c ModelClient, model string) *BatchedTopicGenerator {
	return &BatchedTopicGenerator{c: c, model: model}
}

func (b *BatchedTopicGenerator) BatchTopics(ctx context.Context, chunks []examgen.Chunk) ([][]string, error) {
	if len(chunks) == 0 {
		return [][]string{}, nil
	}

	ctx = WithLabel(ctx, "outline/map")
	var out struct {
		Chunks []struct {
			ID     string   `json:"chunk_id"`
			Topics []string `json:"topics"`
		} `json:"chunks"`
	}
	msgs := []Message{
		{Role: "system", Content: examgen.TopicBatchSystem()},
		{Role: "user", Content: examgen.TopicBatchPrompt(chunks)},
	}
	if err := b.c.ChatJSON(ctx, b.model, msgs, examgen.TopicBatchSchema(), genOptions(32768, 0), &out); err != nil {
		return nil, err
	}

	index := make(map[string]int, len(chunks))
	for i, c := range chunks {
		index[c.ID] = i
	}
	perChunk := make([][]string, len(chunks))
	seen := make(map[string]bool, len(chunks))
	for _, result := range out.Chunks {
		i, ok := index[result.ID]
		if !ok {
			// JSON mode guarantees valid JSON, but not that the model follows
			// the requested set of IDs. Ignore hallucinated IDs and recover the
			// real missing chunks below.
			continue
		}
		if seen[result.ID] {
			continue
		}
		seen[result.ID] = true
		perChunk[i] = result.Topics
	}
	for i, c := range chunks {
		if seen[c.ID] {
			continue
		}
		topics, err := b.topic(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("fallback topics for %s: %w", c.ID, err)
		}
		perChunk[i] = topics
	}
	return perChunk, nil
}

func (b *BatchedTopicGenerator) topic(ctx context.Context, chunk examgen.Chunk) ([]string, error) {
	var out struct {
		Topics []string `json:"topics"`
	}
	msgs := []Message{
		{Role: "system", Content: examgen.TopicSystem()},
		{Role: "user", Content: examgen.TopicPrompt(chunk)},
	}
	if err := b.c.ChatJSON(ctx, b.model, msgs, examgen.TopicSchema(), genOptions(4096, 0), &out); err != nil {
		return nil, err
	}
	return out.Topics, nil
}

var _ examgen.TopicBatcher = (*BatchedTopicGenerator)(nil)
