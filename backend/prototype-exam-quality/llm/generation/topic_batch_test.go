package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"protoexam/llm/providers"
	"strings"
	"testing"

	"protoexam/examgen"
)

func deepSeekTopicRequestIDs(t *testing.T, r *http.Request) []string {
	t.Helper()
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	var ids []string
	for _, message := range body.Messages {
		if message.Role != "user" {
			continue
		}
		for _, line := range strings.Split(message.Content, "\n") {
			if !strings.HasPrefix(line, "Chunk ") {
				continue
			}
			id, _, _ := strings.Cut(strings.TrimPrefix(line, "Chunk "), " ")
			ids = append(ids, id)
		}
	}
	return ids
}

func writeDeepSeekTopics(t *testing.T, w http.ResponseWriter, ids []string) {
	t.Helper()
	chunks := make([]map[string]any, len(ids))
	for i, id := range ids {
		chunks[i] = map[string]any{"chunk_id": id, "kind": "content", "topics": []string{"topic " + id}}
	}
	content, err := json.Marshal(map[string]any{"chunks": chunks})
	if err != nil {
		t.Fatalf("marshal response content: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{"content": string(content)},
		}},
	})
}

func TestBatchedTopicsUsesOneCallAndRestoresOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		generation, ok := body["generationConfig"].(map[string]any)
		if !ok {
			t.Fatalf("generationConfig missing: %#v", body)
		}
		if _, ok := generation["responseJsonSchema"]; !ok {
			t.Fatalf("batch JSON schema missing: %#v", generation)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"chunks\":[{\"chunk_id\":\"p1-c1\",\"topics\":[\"second\"]},{\"chunk_id\":\"p1-c0\",\"topics\":[\"first\"]}]}"}]}}]}`))
	}))
	defer server.Close()

	client := NewGeminiAt(server.URL, "test-key", server.Client())
	batcher := NewBatchedTopicGenerator(client, "gemini-2.5-flash")
	got, err := batcher.BatchTopics(context.Background(), []examgen.Chunk{
		{ID: "p1-c0", Page: 1, Text: "first passage"},
		{ID: "p1-c1", Page: 1, Text: "second passage"},
	}, nil)
	if err != nil {
		t.Fatalf("BatchTopics() error = %v", err)
	}
	if len(got) != 2 || got[0].Topics[0] != "first" || got[1].Topics[0] != "second" {
		t.Fatalf("topics = %#v, want document order", got)
	}
	if calls := client.Stats.Snapshot("outline/map").Calls; calls != 1 {
		t.Fatalf("Gemini API calls = %d, want 1", calls)
	}
}

// The map step, not a phrase list, is what keeps answer keys and rubrics out of
// the evidence graph, so the classification has to survive the wire.
func TestBatchedTopicsCarriesTheClassificationThrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"chunks\":[{\"chunk_id\":\"p1-c0\",\"kind\":\"content\",\"topics\":[\"การย่อยอาหารในปาก\"]},{\"chunk_id\":\"p1-c1\",\"kind\":\"apparatus\",\"topics\":[\"เฉลยแบบฝึกหัดท้ายบทที่ 13\"]}]}"}}]}`))
	}))
	defer server.Close()

	client := providers.NewDeepSeekAt(server.URL, "test-key", server.Client())
	batcher := NewBatchedTopicGenerator(client, "deepseek-chat")
	got, err := batcher.BatchTopics(context.Background(), []examgen.Chunk{
		{ID: "p1-c0", Page: 1, Text: "digestion in the mouth"},
		{ID: "p1-c1", Page: 1, Text: "answers to the chapter exercises"},
	}, nil)
	if err != nil {
		t.Fatalf("BatchTopics() error = %v", err)
	}
	if len(got) != 2 || got[0].Kind != examgen.TopicContent || got[1].Kind != examgen.TopicApparatus {
		t.Fatalf("topics = %#v, want the provider's own classification", got)
	}
	if got[1].Topics[0] != "เฉลยแบบฝึกหัดท้ายบทที่ 13" {
		t.Fatalf("apparatus title = %q, want it kept for the run report", got[1].Topics[0])
	}
}

func TestBatchedTopicsFallsBackForOmittedChunks(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"chunks\":[{\"chunk_id\":\"p1-c1\",\"topics\":[\"second\"]}]}"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"topics\":[\"first\"]}"}}]}`))
	}))
	defer server.Close()

	client := providers.NewDeepSeekAt(server.URL, "test-key", server.Client())
	batcher := NewBatchedTopicGenerator(client, "deepseek-chat")
	got, err := batcher.BatchTopics(context.Background(), []examgen.Chunk{
		{ID: "p1-c0", Page: 1, Text: "first passage"},
		{ID: "p1-c1", Page: 1, Text: "second passage"},
	}, nil)
	if err != nil {
		t.Fatalf("BatchTopics() error = %v", err)
	}
	if len(got) != 2 || got[0].Topics[0] != "first" || got[1].Topics[0] != "second" {
		t.Fatalf("topics = %#v, want fallback plus batch result in document order", got)
	}
	if requests != 2 {
		t.Fatalf("provider requests = %d, want 2", requests)
	}
	if calls := client.Stats.Snapshot("outline/map").Calls; calls != 2 {
		t.Fatalf("recorded API calls = %d, want 2", calls)
	}
}

func TestBatchedTopicsBoundsLargeDocumentRequests(t *testing.T) {
	requests := 0
	maxRequestChunks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		ids := deepSeekTopicRequestIDs(t, r)
		if len(ids) > maxRequestChunks {
			maxRequestChunks = len(ids)
		}
		writeDeepSeekTopics(t, w, ids)
	}))
	defer server.Close()

	chunks := make([]examgen.Chunk, 392)
	for i := range chunks {
		chunks[i] = examgen.Chunk{
			ID:   fmt.Sprintf("p%d-c%d", i+1, i),
			Page: i + 1,
			Text: strings.Repeat("ก", 1200),
		}
	}
	wantCalls := PlannedTopicBatches(chunks)
	client := providers.NewDeepSeekAt(server.URL, "test-key", server.Client())
	batcher := NewBatchedTopicGenerator(client, "deepseek-chat")
	got, err := batcher.BatchTopics(context.Background(), chunks, nil)
	if err != nil {
		t.Fatalf("BatchTopics() error = %v", err)
	}
	if len(got) != len(chunks) {
		t.Fatalf("topics = %d chunks, want %d", len(got), len(chunks))
	}
	if wantCalls <= 1 || requests != wantCalls {
		t.Fatalf("provider requests = %d, planned = %d; want multiple bounded requests", requests, wantCalls)
	}
	if maxRequestChunks > topicBatchMaxChunks {
		t.Fatalf("largest request = %d chunks, limit = %d", maxRequestChunks, topicBatchMaxChunks)
	}
}

func TestBatchedTopicsSplitsOnlyMalformedBatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		ids := deepSeekTopicRequestIDs(t, r)
		if len(ids) > topicBatchMaxChunks/2 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"chunks\":["}}]}`))
			return
		}
		writeDeepSeekTopics(t, w, ids)
	}))
	defer server.Close()

	chunks := make([]examgen.Chunk, topicBatchMaxChunks+1)
	for i := range chunks {
		chunks[i] = examgen.Chunk{ID: fmt.Sprintf("p1-c%d", i), Page: 1, Text: "passage"}
	}
	client := providers.NewDeepSeekAt(server.URL, "test-key", server.Client())
	batcher := NewBatchedTopicGenerator(client, "deepseek-chat")
	got, err := batcher.BatchTopics(context.Background(), chunks, nil)
	if err != nil {
		t.Fatalf("BatchTopics() error = %v", err)
	}
	if len(got) != len(chunks) {
		t.Fatalf("topics = %d chunks, want %d", len(got), len(chunks))
	}
	// Two malformed attempts for the first 32-chunk batch, two successful
	// 16-chunk halves, and the unaffected final one-chunk batch.
	if requests != 5 {
		t.Fatalf("provider requests = %d, want 5 isolated attempts", requests)
	}
}

func TestBatchedTopicsStopsAfterTerminalBatchFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	chunks := make([]examgen.Chunk, topicBatchMaxChunks+1)
	for i := range chunks {
		chunks[i] = examgen.Chunk{ID: fmt.Sprintf("p1-c%d", i), Page: 1, Text: "passage"}
	}
	client := providers.NewDeepSeekAt(server.URL, "test-key", server.Client())
	batcher := NewBatchedTopicGenerator(client, "deepseek-chat")
	_, err := batcher.BatchTopics(context.Background(), chunks, nil)
	if err == nil {
		t.Fatal("BatchTopics() error = nil, want provider failure")
	}
	if requests != 1 {
		t.Fatalf("provider requests = %d, want pending batches cancelled after first terminal failure", requests)
	}
}

func TestGeminiRetries429AndCountsBothAttempts(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"try again"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"ok\":true}"}]}}]}`))
	}))
	defer server.Close()

	client := NewGeminiAt(server.URL, "test-key", server.Client())
	var out struct {
		OK bool `json:"ok"`
	}
	if err := client.ChatJSON(WithLabel(context.Background(), "outline/map"), "gemini-2.5-flash", []Message{{Role: "user", Content: "ok"}}, map[string]any{"type": "object"}, nil, &out); err != nil {
		t.Fatalf("ChatJSON() error = %v", err)
	}
	if !out.OK || requests != 2 {
		t.Fatalf("result=%#v requests=%d, want true and 2", out, requests)
	}
	if calls := client.Stats.Snapshot("outline/map").Calls; calls != 2 {
		t.Fatalf("recorded API calls = %d, want 2", calls)
	}
}
