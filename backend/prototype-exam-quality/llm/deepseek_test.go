package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepSeekChatJSONUsesJSONModeAndCountsCall(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"topics\":[\"algebra\"]}"}}]}`))
	}))
	defer server.Close()

	client := NewDeepSeekAt(server.URL, "test-key", server.Client())
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
	if calls := client.Stats.by["outline/map"].Calls; calls != 1 {
		t.Fatalf("API calls = %d, want 1", calls)
	}
}

func TestDeepSeekChatToolsRoundTripPreservesToolCallID(t *testing.T) {
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

	client := NewDeepSeekAt(server.URL, "test-key", server.Client())
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
