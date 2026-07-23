package main

import (
	"strings"
	"testing"
)

func TestIsResponsesEndpoint(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/v1/responses", true},
		{"/api/v1/responses", true}, // OpenRouter
		{"/openai/responses", true}, // Azure
		{"/v1/chat/completions", false},
		{"/v1/messages", false},
		{"/v1/responses/resp_123", false},
	}
	for _, c := range cases {
		if got := isResponsesEndpoint(c.path); got != c.want {
			t.Errorf("isResponsesEndpoint(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsLLMEndpointResponses(t *testing.T) {
	if !isLLMEndpoint("/v1/responses") {
		t.Error("expected /v1/responses to be an LLM endpoint")
	}
	if !isLLMEndpoint("/api/v1/responses") {
		t.Error("expected /api/v1/responses to be an LLM endpoint")
	}
}

func TestConvertResponsesRequestStringInput(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5",
		"instructions": "You are helpful.",
		"input": "Hello there",
		"stream": true,
		"max_output_tokens": 512,
		"temperature": 0.7
	}`)
	req := convertResponsesRequest(body)
	if req == nil {
		t.Fatal("convertResponsesRequest returned nil")
	}
	if req.Model != "gpt-5" || !req.Stream || req.MaxTokens != 512 || req.Temperature != 0.7 {
		t.Errorf("metadata mismatch: %+v", req)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are helpful." {
		t.Errorf("bad system message: %+v", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "Hello there" {
		t.Errorf("bad user message: %+v", req.Messages[1])
	}
}

func TestConvertResponsesRequestItemArray(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5",
		"input": [
			{"role": "user", "content": [
				{"type": "input_text", "text": "What is in this image?"},
				{"type": "input_image", "image_url": "https://example.com/cat.png"}
			]},
			{"type": "reasoning", "summary": [{"type": "summary_text", "text": "The user wants X"}]},
			{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{\"city\":\"SF\"}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "sunny"}
		]
	}`)
	req := convertResponsesRequest(body)
	if req == nil {
		t.Fatal("convertResponsesRequest returned nil")
	}
	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(req.Messages), req.Messages)
	}

	// Message with text + image
	if req.Messages[0].Role != "user" {
		t.Errorf("expected user role, got %q", req.Messages[0].Role)
	}
	parts, ok := req.Messages[0].Content.([]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("expected 2 content parts, got %#v", req.Messages[0].Content)
	}
	if text := extractOpenAITextContent(req.Messages[0].Content); text != "What is in this image?" {
		t.Errorf("text content mismatch: %q", text)
	}
	imgPart, _ := parts[1].(map[string]interface{})
	if url, _ := extractImageURL(imgPart); url != "https://example.com/cat.png" {
		t.Errorf("image url not converted to chat-completions shape: %#v", parts[1])
	}

	// Reasoning item
	if req.Messages[1].ReasoningContent != "The user wants X" {
		t.Errorf("reasoning content mismatch: %+v", req.Messages[1])
	}

	// Function call item
	tc := req.Messages[2].ToolCalls
	if req.Messages[2].Role != "assistant" || len(tc) != 1 || tc[0].ID != "call_1" ||
		tc[0].Function.Name != "get_weather" || tc[0].Function.Arguments != `{"city":"SF"}` {
		t.Errorf("function_call mismatch: %+v", req.Messages[2])
	}

	// Function call output item
	if req.Messages[3].Role != "tool" || req.Messages[3].ToolCallID != "call_1" || req.Messages[3].Content != "sunny" {
		t.Errorf("function_call_output mismatch: %+v", req.Messages[3])
	}
}

func TestConvertResponsesResponse(t *testing.T) {
	body := []byte(`{
		"id": "resp_abc",
		"object": "response",
		"created_at": 1741476542,
		"model": "gpt-5",
		"status": "completed",
		"output": [
			{"type": "reasoning", "id": "rs_1", "summary": [{"type": "summary_text", "text": "thinking hard"}]},
			{"type": "message", "id": "msg_1", "role": "assistant", "status": "completed",
			 "content": [{"type": "output_text", "text": "The answer is 42.", "annotations": []}]},
			{"type": "function_call", "call_id": "call_9", "name": "lookup", "arguments": "{\"q\":\"x\"}"}
		],
		"usage": {"input_tokens": 120, "output_tokens": 45, "total_tokens": 165}
	}`)
	resp := convertResponsesResponse(body)
	if resp == nil {
		t.Fatal("convertResponsesResponse returned nil")
	}
	if resp.ID != "resp_abc" || resp.Model != "gpt-5" {
		t.Errorf("metadata mismatch: %+v", resp)
	}
	if resp.Usage.PromptTokens != 120 || resp.Usage.CompletionTokens != 45 || resp.Usage.TotalTokens != 165 {
		t.Errorf("usage mismatch: %+v", resp.Usage)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.Message.Role != "assistant" {
		t.Errorf("role mismatch: %q", choice.Message.Role)
	}
	if choice.Message.Content != "The answer is 42." {
		t.Errorf("content mismatch: %#v", choice.Message.Content)
	}
	if choice.Message.ReasoningContent != "thinking hard" {
		t.Errorf("reasoning mismatch: %q", choice.Message.ReasoningContent)
	}
	if len(choice.Message.ToolCalls) != 1 || choice.Message.ToolCalls[0].Function.Name != "lookup" {
		t.Errorf("tool calls mismatch: %+v", choice.Message.ToolCalls)
	}
	if choice.FinishReason != "tool_calls" {
		t.Errorf("finish reason mismatch: %q", choice.FinishReason)
	}
}

func TestConvertResponsesResponseRejectsNonResponses(t *testing.T) {
	if resp := convertResponsesResponse([]byte(`{"choices":[{"message":{"content":"hi"}}]}`)); resp != nil {
		t.Errorf("expected nil for chat-completions body, got %+v", resp)
	}
	if resp := convertResponsesResponse([]byte(`not json`)); resp != nil {
		t.Errorf("expected nil for invalid JSON, got %+v", resp)
	}
}

const responsesSSEComplete = `event: response.created
data: {"type":"response.created","response":{"id":"resp_s1","object":"response","model":"gpt-5","status":"in_progress","output":[]}}

event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello "}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"world"}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello world","annotations":[]}]}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_s1","object":"response","model":"gpt-5","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello world","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}
`

func TestReassembleResponsesSSEComplete(t *testing.T) {
	resp := reassembleResponsesSSEResponse([]byte(responsesSSEComplete))
	if resp == nil {
		t.Fatal("reassembleResponsesSSEResponse returned nil")
	}
	if resp.ID != "resp_s1" || resp.Model != "gpt-5" {
		t.Errorf("metadata mismatch: %+v", resp)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "Hello world" {
		t.Errorf("content mismatch: %+v", resp.Choices)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 3 {
		t.Errorf("usage mismatch: %+v", resp.Usage)
	}
}

const responsesSSEPartial = `event: response.created
data: {"type":"response.created","response":{"id":"resp_s2","object":"response","model":"gpt-5","status":"in_progress","output":[]}}

event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}

event: response.reasoning_summary_text.delta
data: {"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"pondering..."}

event: response.output_item.added
data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":1,"content_index":0,"delta":"Partial ans"}

event: response.output_item.added
data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_5","name":"search","arguments":""}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"q\":"}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"\"go\"}"}
`

func TestReassembleResponsesSSEPartial(t *testing.T) {
	resp := reassembleResponsesSSEResponse([]byte(responsesSSEPartial))
	if resp == nil {
		t.Fatal("reassembleResponsesSSEResponse returned nil for partial stream")
	}
	if resp.ID != "resp_s2" {
		t.Errorf("id mismatch: %q", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	msg := resp.Choices[0].Message
	if msg.ReasoningContent != "pondering..." {
		t.Errorf("reasoning mismatch: %q", msg.ReasoningContent)
	}
	if msg.Content != "Partial ans" {
		t.Errorf("content mismatch: %#v", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_5" ||
		msg.ToolCalls[0].Function.Name != "search" || msg.ToolCalls[0].Function.Arguments != `{"q":"go"}` {
		t.Errorf("tool call mismatch: %+v", msg.ToolCalls)
	}
}

func TestExtractTokenUsageFromResponsesSSE(t *testing.T) {
	req := &LLMRequest{Path: "/v1/responses"}
	cost := extractTokenUsageFromSSE(req, []byte(responsesSSEComplete))
	if req.InputTokens != 10 || req.OutputTokens != 3 {
		t.Errorf("tokens mismatch: in=%d out=%d", req.InputTokens, req.OutputTokens)
	}
	if cost != 0 {
		t.Errorf("unexpected provider cost: %f", cost)
	}
}

func TestResponsesCacheKeyDistinct(t *testing.T) {
	a := GenerateCacheKey("/v1/responses", []byte(`{"model":"gpt-5","input":"first prompt"}`))
	b := GenerateCacheKey("/v1/responses", []byte(`{"model":"gpt-5","input":"second prompt"}`))
	if a == b {
		t.Error("distinct Responses API inputs produced identical cache keys")
	}
	// Identical requests must produce identical keys
	c := GenerateCacheKey("/v1/responses", []byte(`{"model":"gpt-5","input":"first prompt"}`))
	if a != c {
		t.Error("identical Responses API requests produced different cache keys")
	}
}

func TestReassembleResponsesSSEFailedKeepsAccumulatedText(t *testing.T) {
	// A response.failed terminal event with empty output must not discard the
	// text accumulated from deltas, and the error must be surfaced.
	stream := `data: {"type":"response.created","response":{"id":"resp_f1","object":"response","model":"gpt-5","status":"in_progress","output":[]}}
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}
data: {"type":"response.output_text.delta","output_index":0,"delta":"partial text here"}
data: {"type":"response.failed","response":{"id":"resp_f1","object":"response","model":"gpt-5","status":"failed","output":[],"error":{"code":"server_error","message":"upstream exploded"},"usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}}
`
	resp := reassembleResponsesSSEResponse([]byte(stream))
	if resp == nil {
		t.Fatal("reassembleResponsesSSEResponse returned nil")
	}
	content, _ := resp.Choices[0].Message.Content.(string)
	if !strings.Contains(content, "partial text here") {
		t.Errorf("accumulated text lost: %q", content)
	}
	if !strings.Contains(content, "[error] upstream exploded") {
		t.Errorf("terminal error not surfaced: %q", content)
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 2 {
		t.Errorf("usage from terminal event not merged: %+v", resp.Usage)
	}
	if resp.Choices[0].FinishReason != "failed" {
		t.Errorf("finish reason mismatch: %q", resp.Choices[0].FinishReason)
	}
}

func TestReassembleResponsesSSERefusalMarker(t *testing.T) {
	stream := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}
data: {"type":"response.refusal.delta","output_index":0,"delta":"I cannot help with that."}
`
	resp := reassembleResponsesSSEResponse([]byte(stream))
	if resp == nil {
		t.Fatal("reassembleResponsesSSEResponse returned nil")
	}
	content, _ := resp.Choices[0].Message.Content.(string)
	if !strings.Contains(content, "[refusal] I cannot help with that.") {
		t.Errorf("streamed refusal missing [refusal] marker: %q", content)
	}
}

func TestConvertResponsesRequestNullInput(t *testing.T) {
	req := convertResponsesRequest([]byte(`{"model":"gpt-5","input":null,"instructions":"hi"}`))
	if req == nil {
		t.Fatal("convertResponsesRequest returned nil")
	}
	if len(req.Messages) != 1 {
		t.Errorf("null input fabricated a message: %+v", req.Messages)
	}
}

func TestConvertResponsesResponseFractionalCreatedAt(t *testing.T) {
	body := []byte(`{"id":"resp_x","object":"response","created_at":1741476542.5,"model":"gpt-5","status":"completed",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`)
	resp := convertResponsesResponse(body)
	if resp == nil {
		t.Fatal("fractional created_at broke response parsing")
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("content mismatch: %#v", resp.Choices[0].Message.Content)
	}
}

func TestResponsesCacheKeyOutputShapingParams(t *testing.T) {
	base := GenerateCacheKey("/v1/responses", []byte(`{"model":"m","input":"hi"}`))
	jsonSchema := GenerateCacheKey("/v1/responses", []byte(`{"model":"m","input":"hi","text":{"format":{"type":"json_schema"}}}`))
	reasoning := GenerateCacheKey("/v1/responses", []byte(`{"model":"m","input":"hi","reasoning":{"effort":"high"}}`))
	if base == jsonSchema {
		t.Error("text.format ignored in cache key")
	}
	if base == reasoning || jsonSchema == reasoning {
		t.Error("reasoning.effort ignored in cache key")
	}
}

func TestResponsesRequestPreviewSnippet(t *testing.T) {
	body := []byte(`{"model":"gpt-5","instructions":"Be terse.","input":"Summarize this document"}`)
	snippet := extractRequestPreviewSnippet("/v1/responses", body)
	if !strings.Contains(snippet, "Be terse.") {
		t.Errorf("expected instructions in preview snippet, got %q", snippet)
	}
}

func TestResponsesStreamingResponsePreviewSnippet(t *testing.T) {
	snippet := extractStreamingResponsePreviewSnippet([]byte(responsesSSEComplete))
	if snippet != "Hello world" {
		t.Errorf("expected reassembled text preview, got %q", snippet)
	}
}
