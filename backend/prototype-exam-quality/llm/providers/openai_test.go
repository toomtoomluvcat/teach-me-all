package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatibleChatJSONUsesJSONModeAndCountsCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		format, ok := body["response_format"].(map[string]any)
		if !ok || format["type"] != "json_object" {
			t.Fatalf("JSON mode missing: %#v", body["response_format"])
		}
		messages, _ := body["messages"].([]any)
		encoded, _ := json.Marshal(messages)
		if !strings.Contains(strings.ToLower(string(encoded)), "json") {
			t.Fatalf("JSON instruction missing from messages: %s", encoded)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"topics\":[\"algebra\"]}"}}],"usage":{"prompt_tokens":123,"completion_tokens":17,"total_tokens":140}}`))
	}))
	defer server.Close()

	client := NewOpenAICompatibleAt(server.URL, "test-key", server.Client())
	var out struct {
		Topics []string `json:"topics"`
	}
	err := client.ChatJSON(WithLabel(context.Background(), "outline/map"), "deepseek-chat", []Message{{Role: "user", Content: "Name a topic."}}, nil, nil, &out)
	if err != nil {
		t.Fatalf("ChatJSON() error = %v", err)
	}
	if len(out.Topics) != 1 || out.Topics[0] != "algebra" {
		t.Fatalf("decoded output = %#v", out)
	}
	if calls := client.Stats.Snapshot("outline/map").Calls; calls != 1 {
		t.Fatalf("API calls = %d, want 1", calls)
	}
	if bucket := client.Stats.Snapshot("outline/map"); bucket.PromptTokens != 123 || bucket.EvalTokens != 17 {
		t.Fatalf("recorded tokens = %d/%d, want 123/17", bucket.PromptTokens, bucket.EvalTokens)
	}
}

func TestOpenAICompatibleChatToolsRoundTripPreservesToolCallID(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if requests == 1 {
			if _, ok := body["tools"]; !ok {
				t.Fatalf("tools missing")
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"calc","arguments":"{\"expression\":\"2+2\"}"}}]}}]}`))
			return
		}
		messages, _ := body["messages"].([]any)
		encoded, _ := json.Marshal(messages)
		if !strings.Contains(string(encoded), `"tool_call_id":"call-1"`) {
			t.Fatalf("tool_call_id missing: %s", encoded)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"DONE"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatibleAt(server.URL, "test-key", server.Client())
	tools := []Tool{{Type: "function", Function: ToolFunction{Name: "calc", Description: "calculate", Parameters: map[string]any{"type": "object"}}}}
	first, err := client.ChatTools(context.Background(), "deepseek-chat", []Message{{Role: "user", Content: "calculate"}}, tools, nil)
	if err != nil {
		t.Fatalf("first ChatTools() error = %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "call-1" {
		t.Fatalf("tool call = %#v", first.ToolCalls)
	}
	second, err := client.ChatTools(context.Background(), "deepseek-chat", []Message{
		{Role: "user", Content: "calculate"},
		{Role: "assistant", ToolCalls: first.ToolCalls},
		{Role: "tool", ToolCallID: first.ToolCalls[0].ID, ToolName: "calc", Content: "4"},
	}, tools, nil)
	if err != nil {
		t.Fatalf("second ChatTools() error = %v", err)
	}
	if second.Content != "DONE" || requests != 2 {
		t.Fatalf("second response=%q requests=%d", second.Content, requests)
	}
}

func TestOpenAICompatibleEmbedMapsVectorsBackByIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "bge-m3" || len(body.Input) != 2 {
			t.Fatalf("request = %#v", body)
		}
		// Out of order on purpose: the response carries an index, and the caller
		// pairs vectors with its own inputs by that index rather than by arrival.
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0.5,0.25]},{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatibleAt(server.URL, "", server.Client())
	vecs, err := client.Embed(context.Background(), "bge-m3", []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vecs) != 2 || vecs[0][0] != 1 || vecs[1][0] != 0.5 {
		t.Fatalf("vectors = %#v, want them restored to input order", vecs)
	}
}

func TestOpenAICompatibleEmbedRejectsAShortReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatibleAt(server.URL, "", server.Client())
	if _, err := client.Embed(context.Background(), "bge-m3", []string{"a", "b"}); err == nil {
		t.Fatal("Embed() accepted one vector for two inputs; the duplicate gate would silently lose a stem")
	}
}
