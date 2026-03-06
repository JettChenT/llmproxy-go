package main

import (
	"strings"
	"testing"
)

func TestOutputCopyText_OpenAIJSONCopiesAssistantTextOnly(t *testing.T) {
	req := &LLMRequest{
		Path:         "/v1/chat/completions",
		ResponseBody: []byte(`{"id":"chatcmpl_1","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"Hello\nworld"},"finish_reason":"stop"}]}`),
	}

	got, label, err := outputCopyText(req)
	if err != nil {
		t.Fatalf("outputCopyText error = %v", err)
	}
	if label != "output" {
		t.Fatalf("label = %q, want output", label)
	}
	want := "Hello\nworld"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOutputCopyText_OpenAISSECopiesReconstructedText(t *testing.T) {
	req := &LLMRequest{
		Path: "/v1/chat/completions",
		ResponseBody: []byte("data: {\"id\":\"chatcmpl_1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello \"}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"}}]}\n\n" +
			"data: [DONE]\n\n"),
	}

	got, _, err := outputCopyText(req)
	if err != nil {
		t.Fatalf("outputCopyText error = %v", err)
	}
	if got != "Hello world" {
		t.Fatalf("got %q, want %q", got, "Hello world")
	}
}

func TestOutputCopyText_OpenAIToolCallCopiesToolCallPayload(t *testing.T) {
	req := &LLMRequest{
		Path:         "/v1/chat/completions",
		ResponseBody: []byte(`{"id":"chatcmpl_1","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Write","arguments":"{\"path\":\"/tmp/x.txt\"}"}}]},"finish_reason":"tool_calls"}]}`),
	}

	got, _, err := outputCopyText(req)
	if err != nil {
		t.Fatalf("outputCopyText error = %v", err)
	}
	if got == "" {
		t.Fatal("expected copied tool call payload, got empty string")
	}
	if !containsAll(got, []string{"\"name\": \"Write\"", "\\\"path\\\":\\\"/tmp/x.txt\\\""}) {
		t.Fatalf("copied output missing tool call payload: %q", got)
	}
}

func TestOutputCopyText_AnthropicCopiesTextOnly(t *testing.T) {
	req := &LLMRequest{
		Path:         "/v1/messages",
		ResponseBody: []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"Done."}],"usage":{"input_tokens":10,"output_tokens":3}}`),
	}

	got, _, err := outputCopyText(req)
	if err != nil {
		t.Fatalf("outputCopyText error = %v", err)
	}
	if got != "Done." {
		t.Fatalf("got %q, want %q", got, "Done.")
	}
}

func containsAll(s string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
