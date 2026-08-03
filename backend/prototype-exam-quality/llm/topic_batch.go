package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"protoexam/examgen"
)

const (
	topicBatchMaxChunks = 32
	topicBatchMaxRunes  = 36_000
)

// BatchedTopicGenerator uses bounded provider requests instead of either one
// request per chunk or one document-sized request. The normal Generator.Topics
// path remains unchanged for Ollama and callers that do not opt in.
type BatchedTopicGenerator struct {
	c        ModelClient
	model    string
	parallel int
}

func NewBatchedTopicGenerator(c ModelClient, model string, parallel ...int) *BatchedTopicGenerator {
	slots := 1
	if len(parallel) > 0 && parallel[0] > 1 {
		slots = parallel[0]
	}
	return &BatchedTopicGenerator{c: c, model: model, parallel: slots}
}

type topicBatch struct {
	start  int
	chunks []examgen.Chunk
}

func planTopicBatches(chunks []examgen.Chunk) []topicBatch {
	var batches []topicBatch
	for start := 0; start < len(chunks); {
		end := start
		runes := 0
		for end < len(chunks) && end-start < topicBatchMaxChunks {
			// Include the labelled-prompt framing in the budget. It is small but
			// counting it keeps the limit meaningful for unusually long IDs.
			next := examgen.RuneLen(chunks[end].Text) + examgen.RuneLen(chunks[end].ID) + 32
			if end > start && runes+next > topicBatchMaxRunes {
				break
			}
			runes += next
			end++
		}
		batches = append(batches, topicBatch{start: start, chunks: chunks[start:end]})
		start = end
	}
	return batches
}

// PlannedTopicBatches reports the normal map-call count before any provider
// request is made. Malformed-output recovery and omitted-chunk fallback can add
// calls, and the final outline reduce is one separate call.
func PlannedTopicBatches(chunks []examgen.Chunk) int {
	return len(planTopicBatches(chunks))
}

func (b *BatchedTopicGenerator) BatchTopics(ctx context.Context, chunks []examgen.Chunk, progress examgen.Progress) ([]examgen.ChunkTopics, error) {
	if len(chunks) == 0 {
		return []examgen.ChunkTopics{}, nil
	}

	ctx = WithLabel(ctx, "outline/map")
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	batches := planTopicBatches(chunks)
	results := make([][]examgen.ChunkTopics, len(batches))
	sem := make(chan struct{}, b.parallel)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		done     int
		firstErr error
	)
	for i, batch := range batches {
		wg.Add(1)
		go func(i int, batch topicBatch) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-batchCtx.Done():
				return
			}
			defer func() { <-sem }()
			if batchCtx.Err() != nil {
				return
			}

			mapped, err := b.mapBatch(batchCtx, batch.chunks)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("batch %d/%d: %w", i+1, len(batches), err)
					cancel()
				}
				return
			}
			results[i] = mapped
			done += len(batch.chunks)
			if progress != nil {
				progress("outline/map", done, len(chunks), fmt.Sprintf("batch %d/%d", i+1, len(batches)))
			}
		}(i, batch)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	perChunk := make([]examgen.ChunkTopics, len(chunks))
	for i, batch := range batches {
		copy(perChunk[batch.start:batch.start+len(batch.chunks)], results[i])
	}
	return perChunk, nil
}

func (b *BatchedTopicGenerator) mapBatch(ctx context.Context, chunks []examgen.Chunk) ([]examgen.ChunkTopics, error) {
	mapped, err := b.requestBatch(ctx, chunks)
	if err == nil {
		return mapped, nil
	}
	if len(chunks) == 1 || !isMalformedTopicJSON(err) {
		return nil, err
	}

	// Retrying the same oversized shape cannot restore bytes the provider did
	// not return. Split only this failed batch; successful sibling batches are
	// untouched and remain usable.
	mid := len(chunks) / 2
	left, leftErr := b.mapBatch(ctx, chunks[:mid])
	if leftErr != nil {
		return nil, leftErr
	}
	right, rightErr := b.mapBatch(ctx, chunks[mid:])
	if rightErr != nil {
		return nil, rightErr
	}
	return append(left, right...), nil
}

func isMalformedTopicJSON(err error) bool {
	message := strings.ToLower(err.Error())
	return !strings.Contains(message, "fallback topics") &&
		strings.Contains(message, "unparseable json")
}

func (b *BatchedTopicGenerator) requestBatch(ctx context.Context, chunks []examgen.Chunk) ([]examgen.ChunkTopics, error) {
	var out struct {
		Chunks []struct {
			ID     string              `json:"chunk_id"`
			Kind   string              `json:"kind"`
			Topics []string            `json:"topics"`
		} `json:"chunks"`
	}
	msgs := []Message{
		{Role: "system", Content: examgen.TopicBatchSystem()},
		{Role: "user", Content: examgen.TopicBatchPrompt(chunks)},
	}
	if err := b.c.ChatJSON(ctx, b.model, msgs, examgen.TopicBatchSchema(), genOptions(8192, 0), &out); err != nil {
		return nil, err
	}

	index := make(map[string]int, len(chunks))
	for i, c := range chunks {
		index[c.ID] = i
	}
	perChunk := make([]examgen.ChunkTopics, len(chunks))
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
		perChunk[i] = examgen.NewChunkTopics(result.Kind, result.Topics)
	}
	for i, c := range chunks {
		if seen[c.ID] {
			continue
		}
		labelled, err := b.topic(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("fallback topics for %s: %w", c.ID, err)
		}
		perChunk[i] = labelled
	}
	return perChunk, nil
}

func (b *BatchedTopicGenerator) topic(ctx context.Context, chunk examgen.Chunk) (examgen.ChunkTopics, error) {
	var out examgen.ChunkTopics
	msgs := []Message{
		{Role: "system", Content: examgen.TopicSystem()},
		{Role: "user", Content: examgen.TopicPrompt(chunk)},
	}
	if err := b.c.ChatJSON(ctx, b.model, msgs, examgen.TopicSchema(), genOptions(4096, 0), &out); err != nil {
		return examgen.ChunkTopics{}, err
	}
	return out, nil
}

var _ examgen.TopicBatcher = (*BatchedTopicGenerator)(nil)
