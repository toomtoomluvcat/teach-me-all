package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"protoexam/examgen"
)

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
	})
	if err != nil {
		t.Fatalf("BatchTopics() error = %v", err)
	}
	if len(got) != 2 || got[0][0] != "first" || got[1][0] != "second" {
		t.Fatalf("topics = %#v, want document order", got)
	}
	if calls := client.Stats.by["outline/map"].Calls; calls != 1 {
		t.Fatalf("Gemini API calls = %d, want 1", calls)
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

	client := NewDeepSeekAt(server.URL, "test-key", server.Client())
	batcher := NewBatchedTopicGenerator(client, "deepseek-chat")
	got, err := batcher.BatchTopics(context.Background(), []examgen.Chunk{
		{ID: "p1-c0", Page: 1, Text: "first passage"},
		{ID: "p1-c1", Page: 1, Text: "second passage"},
	})
	if err != nil {
		t.Fatalf("BatchTopics() error = %v", err)
	}
	if len(got) != 2 || got[0][0] != "first" || got[1][0] != "second" {
		t.Fatalf("topics = %#v, want fallback plus batch result in document order", got)
	}
	if requests != 2 {
		t.Fatalf("provider requests = %d, want 2", requests)
	}
	if calls := client.Stats.by["outline/map"].Calls; calls != 2 {
		t.Fatalf("recorded API calls = %d, want 2", calls)
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
	if calls := client.Stats.by["outline/map"].Calls; calls != 2 {
		t.Fatalf("recorded API calls = %d, want 2", calls)
	}
}
