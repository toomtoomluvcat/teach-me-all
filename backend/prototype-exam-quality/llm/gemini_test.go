package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiChatJSONUsesStructuredOutputAndCountsCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Fatalf("missing Gemini API key header")
		}
		if r.URL.Path != "/v1beta/models/gemini-2.5-flash:generateContent" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		generation, ok := body["generationConfig"].(map[string]any)
		if !ok || generation["responseMimeType"] != "application/json" {
			t.Fatalf("structured output config missing: %#v", body["generationConfig"])
		}
		if _, ok := generation["responseJsonSchema"]; !ok {
			t.Fatalf("responseJsonSchema missing: %#v", generation)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"topics\":[\"photosynthesis\"]}"}]}}]}`))
	}))
	defer server.Close()

	client := NewGeminiAt(server.URL, "test-key", server.Client())
	var out struct {
		Topics []string `json:"topics"`
	}
	err := client.ChatJSON(WithLabel(context.Background(), "outline/map"), "gemini-2.5-flash", []Message{
		{Role: "system", Content: "Return JSON."},
		{Role: "user", Content: "Name the topic."},
	}, map[string]any{"type": "object"}, &Options{NumPredict: 100}, &out)
	if err != nil {
		t.Fatalf("ChatJSON() error = %v", err)
	}
	if len(out.Topics) != 1 || out.Topics[0] != "photosynthesis" {
		t.Fatalf("decoded output = %#v", out)
	}
	if got := client.Stats.by["outline/map"].Calls; got != 1 {
		t.Fatalf("API calls = %d, want 1", got)
	}
}

func TestGeminiEmbedUsesOneBatchCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-embedding-001:batchEmbedContents" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body struct {
			Requests []map[string]any `json:"requests"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.Requests) != 2 {
			t.Fatalf("embedding requests = %d, want 2", len(body.Requests))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[{"values":[1,0]},{"values":[0,1]}]}`))
	}))
	defer server.Close()

	client := NewGeminiAt(server.URL, "test-key", server.Client())
	vecs, err := client.Embed(context.Background(), "gemini-embedding-001", []string{"one", "two"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 2 || vecs[0][0] != 1 || vecs[1][1] != 1 {
		t.Fatalf("embeddings = %#v", vecs)
	}
	if got := client.Stats.by["embed"].Calls; got != 1 {
		t.Fatalf("API calls = %d, want 1", got)
	}
}

func TestGeminiChatToolsMapsFunctionCallAndResponse(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if requests == 1 {
			tools, ok := body["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("tools missing from first request")
			}
			tool, ok := tools[0].(map[string]any)
			if !ok {
				t.Fatalf("tool has unexpected shape: %#v", tools[0])
			}
			if _, ok := tool["functionDeclarations"]; !ok {
				t.Fatalf("functionDeclarations missing from first request: %#v", tool)
			}
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"calc","args":{"expression":"2+2"}}}]}}]}`))
			return
		}
		contents, _ := body["contents"].([]any)
		encoded, _ := json.Marshal(contents)
		if !strings.Contains(string(encoded), "functionResponse") {
			t.Fatalf("function response missing from second request: %s", encoded)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"DONE"}]}}]}`))
	}))
	defer server.Close()

	client := NewGeminiAt(server.URL, "test-key", server.Client())
	tools := []Tool{{Type: "function", Function: ToolFunction{Name: "calc", Description: "calculate", Parameters: map[string]any{"type": "object"}}}}
	first, err := client.ChatTools(context.Background(), "gemini-2.5-flash", []Message{{Role: "user", Content: "calculate"}}, tools, nil)
	if err != nil {
		t.Fatalf("first ChatTools() error = %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "calc" {
		t.Fatalf("function call = %#v", first.ToolCalls)
	}
	second, err := client.ChatTools(context.Background(), "gemini-2.5-flash", []Message{
		{Role: "user", Content: "calculate"},
		{Role: "assistant", ToolCalls: first.ToolCalls},
		{Role: "tool", ToolName: "calc", Content: "4"},
	}, tools, nil)
	if err != nil {
		t.Fatalf("second ChatTools() error = %v", err)
	}
	if second.Content != "DONE" || requests != 2 {
		t.Fatalf("second response=%q requests=%d", second.Content, requests)
	}
}
