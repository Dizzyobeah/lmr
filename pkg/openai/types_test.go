package openai

import (
	"encoding/json"
	"testing"
)

func TestChatMessageUnmarshalContent(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		want       string
		wantImages []string
	}{
		{
			name: "string content",
			json: `{"role":"user","content":"hello world"}`,
			want: "hello world",
		},
		{
			name: "array of text parts",
			json: `{"role":"user","content":[{"type":"text","text":"line one"},{"type":"text","text":"line two"}]}`,
			want: "line one\nline two",
		},
		{
			name:       "mixed text and image_url",
			json:       `{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}`,
			want:       "describe this",
			wantImages: []string{"data:image/png;base64,AAAA"},
		},
		{
			name:       "image only",
			json:       `{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,BBBB"}}]}`,
			want:       "",
			wantImages: []string{"data:image/jpeg;base64,BBBB"},
		},
		{
			name:       "multiple images",
			json:       `{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,ONE"}},{"type":"image_url","image_url":{"url":"data:image/png;base64,TWO"}}]}`,
			want:       "",
			wantImages: []string{"data:image/png;base64,ONE", "data:image/png;base64,TWO"},
		},
		{
			name: "null content",
			json: `{"role":"assistant","content":null}`,
			want: "",
		},
		{
			name: "absent content",
			json: `{"role":"assistant","tool_call_id":"abc"}`,
			want: "",
		},
		{
			name: "empty array",
			json: `{"role":"user","content":[]}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m ChatMessage
			if err := json.Unmarshal([]byte(tt.json), &m); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.Content != tt.want {
				t.Errorf("Content = %q, want %q", m.Content, tt.want)
			}
			if len(m.Images) != len(tt.wantImages) {
				t.Fatalf("Images = %v, want %v", m.Images, tt.wantImages)
			}
			for i := range tt.wantImages {
				if m.Images[i] != tt.wantImages[i] {
					t.Errorf("Images[%d] = %q, want %q", i, m.Images[i], tt.wantImages[i])
				}
			}
		})
	}
}

func TestChatMessageUnmarshalPreservesOtherFields(t *testing.T) {
	const in = `{"role":"tool","content":"result","tool_call_id":"call_123","name":"lookup"}`
	var m ChatMessage
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Role != "tool" {
		t.Errorf("Role = %q, want tool", m.Role)
	}
	if m.ToolCallID != "call_123" {
		t.Errorf("ToolCallID = %q, want call_123", m.ToolCallID)
	}
	if m.Name != "lookup" {
		t.Errorf("Name = %q, want lookup", m.Name)
	}
	if m.Content != "result" {
		t.Errorf("Content = %q, want result", m.Content)
	}
}

func TestChatCompletionRequestUnmarshalArrayContent(t *testing.T) {
	const in = `{"model":"auto","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].Content != "hi" {
		t.Errorf("Content = %q, want hi", req.Messages[0].Content)
	}
}

// TestChatCompletionRequestPlanMode mirrors a realistic OpenCode plan-mode
// request: a system-reminder, array-shaped content across system/user/assistant/
// tool roles (the shape that previously failed with "cannot unmarshal array into
// ...messages.content of type string"), an image part, and a tools array. It
// asserts the whole request decodes without error and that text/images/tool
// fields are preserved.
func TestChatCompletionRequestPlanMode(t *testing.T) {
	const in = `{
		"model": "auto",
		"messages": [
			{"role": "system", "content": [{"type": "text", "text": "You are in plan mode."}]},
			{"role": "user", "content": [
				{"type": "text", "text": "Review this"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,ZZZ"}}
			]},
			{"role": "assistant", "content": [{"type": "text", "text": "Here is the plan"}], "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "read", "arguments": "{\"path\":\"main.go\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": [{"type": "text", "text": "file contents"}]}
		],
		"tools": [
			{"type": "function", "function": {"name": "read", "description": "read a file", "parameters": {"type": "object"}}}
		]
	}`

	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unexpected error decoding plan-mode request: %v", err)
	}

	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(req.Messages))
	}

	if got := req.Messages[0].Content; got != "You are in plan mode." {
		t.Errorf("system Content = %q", got)
	}

	user := req.Messages[1]
	if user.Content != "Review this" {
		t.Errorf("user Content = %q, want %q", user.Content, "Review this")
	}
	if len(user.Images) != 1 || user.Images[0] != "data:image/png;base64,ZZZ" {
		t.Errorf("user Images = %v", user.Images)
	}

	assistant := req.Messages[2]
	if assistant.Content != "Here is the plan" {
		t.Errorf("assistant Content = %q", assistant.Content)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Name != "read" {
		t.Errorf("assistant ToolCalls = %+v", assistant.ToolCalls)
	}

	tool := req.Messages[3]
	if tool.Content != "file contents" {
		t.Errorf("tool Content = %q", tool.Content)
	}
	if tool.ToolCallID != "call_1" {
		t.Errorf("tool ToolCallID = %q, want call_1", tool.ToolCallID)
	}

	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read" {
		t.Errorf("Tools = %+v", req.Tools)
	}
}
