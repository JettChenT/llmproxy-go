package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// getFreePort finds a free TCP port for testing
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// resetTestState resets global proxy state between tests
func resetTestState() {
	requestsMu.Lock()
	requests = nil
	requestID = 0
	requestsMu.Unlock()
}

// waitForRequest waits for a request with the given ID to complete
func waitForRequest(t *testing.T, id int, timeout time.Duration) *LLMRequest {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		requestsMu.RLock()
		for _, req := range requests {
			if req.ID == id && req.Status != StatusPending {
				requestsMu.RUnlock()
				return req
			}
		}
		requestsMu.RUnlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Timed out waiting for request %d to complete", id)
	return nil
}

// --- Endpoint Detection Tests ---

func TestIsLLMEndpoint(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{"/v1/messages", true},
		{"/v1/chat/completions", true},
		{"/v1/completions", true},
		{"/v1/embeddings", true},
		{"/chat/completions", true},
		{"/completions", true},
		{"/v1/models", false},
		{"/health", false},
		{"/other", false},
	}
	for _, tc := range tests {
		if got := isLLMEndpoint(tc.path); got != tc.expect {
			t.Errorf("isLLMEndpoint(%q) = %v, want %v", tc.path, got, tc.expect)
		}
	}
}

func TestIsAnthropicEndpoint(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{"/v1/messages", true},
		{"/api/v1/messages", true},
		{"/v1/chat/completions", false},
		{"/v1/completions", false},
		{"/messages", false},
	}
	for _, tc := range tests {
		if got := isAnthropicEndpoint(tc.path); got != tc.expect {
			t.Errorf("isAnthropicEndpoint(%q) = %v, want %v", tc.path, got, tc.expect)
		}
	}
}

// --- Token Extraction Tests ---

func TestExtractTokenUsage_Anthropic(t *testing.T) {
	req := &LLMRequest{
		ID:    1,
		Model: "claude-sonnet-4-20250514",
	}

	responseBody := []byte(`{
		"id": "msg_01XFDUDYJgAACzvnptvVoYEL",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 25,
			"output_tokens": 150
		}
	}`)

	extractTokenUsage(req, responseBody)

	if req.InputTokens != 25 {
		t.Errorf("InputTokens = %d, want 25", req.InputTokens)
	}
	if req.OutputTokens != 150 {
		t.Errorf("OutputTokens = %d, want 150", req.OutputTokens)
	}
}

func TestExtractTokenUsage_OpenAI(t *testing.T) {
	req := &LLMRequest{
		ID:    1,
		Model: "gpt-4o",
	}

	responseBody := []byte(`{
		"id": "chatcmpl-test123",
		"object": "chat.completion",
		"model": "gpt-4o",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hi"}, "finish_reason": "stop"}],
		"usage": {
			"prompt_tokens": 15,
			"completion_tokens": 20,
			"total_tokens": 35
		}
	}`)

	extractTokenUsage(req, responseBody)

	if req.InputTokens != 15 {
		t.Errorf("InputTokens = %d, want 15", req.InputTokens)
	}
	if req.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, want 20", req.OutputTokens)
	}
}

// --- Cache Key Tests ---

func TestCacheKey_Anthropic(t *testing.T) {
	req1 := `{"model":"claude-sonnet-4-20250514","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}`
	req2 := `{"model":"claude-sonnet-4-20250514","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}`

	key1 := GenerateCacheKey("/v1/messages", []byte(req1))
	key2 := GenerateCacheKey("/v1/messages", []byte(req2))

	if key1 != key2 {
		t.Errorf("Identical Anthropic requests should have the same cache key\nkey1=%s\nkey2=%s", key1, key2)
	}

	// Different messages → different key
	req3 := `{"model":"claude-sonnet-4-20250514","max_tokens":1024,"messages":[{"role":"user","content":"Goodbye"}]}`
	key3 := GenerateCacheKey("/v1/messages", []byte(req3))

	if key1 == key3 {
		t.Error("Different messages should produce different cache keys")
	}

	// Different system prompts → different key
	req4 := `{"model":"claude-sonnet-4-20250514","max_tokens":1024,"system":"Be helpful","messages":[{"role":"user","content":"Hello"}]}`
	key4 := GenerateCacheKey("/v1/messages", []byte(req4))

	if key1 == key4 {
		t.Error("Different system prompts should produce different cache keys")
	}
}

func TestCacheKey_OpenAI(t *testing.T) {
	req1 := `{"model":"gpt-4o","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}`
	req2 := `{"model":"gpt-4o","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}`

	key1 := GenerateCacheKey("/v1/chat/completions", []byte(req1))
	key2 := GenerateCacheKey("/v1/chat/completions", []byte(req2))

	if key1 != key2 {
		t.Errorf("Identical OpenAI requests should have the same cache key\nkey1=%s\nkey2=%s", key1, key2)
	}

	req3 := `{"model":"gpt-4o","max_tokens":1024,"messages":[{"role":"user","content":"Goodbye"}]}`
	key3 := GenerateCacheKey("/v1/chat/completions", []byte(req3))

	if key1 == key3 {
		t.Error("Different OpenAI requests should produce different cache keys")
	}
}

// --- Integration Tests ---

func TestAnthropicProxyIntegration(t *testing.T) {
	resetTestState()

	// Create mock Anthropic backend
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("Expected path /v1/messages, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req AnthropicRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("Failed to parse Anthropic request: %v", err)
		}
		if req.Model != "claude-sonnet-4-20250514" {
			t.Errorf("Expected model claude-sonnet-4-20250514, got %s", req.Model)
		}
		if req.MaxTokens != 1024 {
			t.Errorf("Expected max_tokens 1024, got %d", req.MaxTokens)
		}

		resp := AnthropicResponse{
			ID:         "msg_test123",
			Type:       "message",
			Role:       "assistant",
			Content:    []AnthropicContentBlock{{Type: "text", Text: "Hello from Claude!"}},
			Model:      "claude-sonnet-4-20250514",
			StopReason: "end_turn",
			Usage:      AnthropicUsage{InputTokens: 10, OutputTokens: 25},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-anthropic", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	// Send Anthropic request
	anthropicReq := AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "Hello, Claude"},
		},
	}
	reqBody, _ := json.Marshal(anthropicReq)

	proxyURL := fmt.Sprintf("http://localhost:%d/v1/messages", port)
	httpReq, _ := http.NewRequest("POST", proxyURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", "test-key")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify response body
	var anthropicResp AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if anthropicResp.ID != "msg_test123" {
		t.Errorf("Response ID = %q, want %q", anthropicResp.ID, "msg_test123")
	}
	if len(anthropicResp.Content) != 1 || anthropicResp.Content[0].Text != "Hello from Claude!" {
		t.Errorf("Unexpected response content: %+v", anthropicResp.Content)
	}

	// Wait for request capture
	captured := waitForRequest(t, 1, 2*time.Second)

	if captured.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Captured model = %q, want %q", captured.Model, "claude-sonnet-4-20250514")
	}
	if captured.StatusCode != 200 {
		t.Errorf("Captured status = %d, want 200", captured.StatusCode)
	}
	if captured.Status != StatusComplete {
		t.Errorf("Captured status = %d, want StatusComplete", captured.Status)
	}

	// Wait for async token extraction
	time.Sleep(200 * time.Millisecond)

	if captured.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", captured.InputTokens)
	}
	if captured.OutputTokens != 25 {
		t.Errorf("OutputTokens = %d, want 25", captured.OutputTokens)
	}
}

func TestOpenAIProxyIntegration(t *testing.T) {
	resetTestState()

	// Create mock OpenAI backend
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		resp := OpenAIResponse{
			ID:      "chatcmpl-test123",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "gpt-4o",
			Choices: []OpenAIChoice{{
				Index:        0,
				Message:      OpenAIMessage{Role: "assistant", Content: "Hello from GPT!"},
				FinishReason: "stop",
			}},
			Usage: OpenAIUsage{PromptTokens: 15, CompletionTokens: 20, TotalTokens: 35},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-openai", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	openaiReq := OpenAIRequest{
		Model:     "gpt-4o",
		MaxTokens: 1024,
		Messages: []OpenAIMessage{
			{Role: "user", Content: "Hello, GPT"},
		},
	}
	reqBody, _ := json.Marshal(openaiReq)

	proxyURL := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	httpReq, _ := http.NewRequest("POST", proxyURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-key")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	captured := waitForRequest(t, 1, 2*time.Second)

	if captured.Model != "gpt-4o" {
		t.Errorf("Captured model = %q, want %q", captured.Model, "gpt-4o")
	}
	if captured.StatusCode != 200 {
		t.Errorf("Captured status = %d, want 200", captured.StatusCode)
	}

	time.Sleep(200 * time.Millisecond)

	if captured.InputTokens != 15 {
		t.Errorf("InputTokens = %d, want 15", captured.InputTokens)
	}
	if captured.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, want 20", captured.OutputTokens)
	}
}

func TestAnthropicProxyWithToolUse(t *testing.T) {
	resetTestState()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := AnthropicResponse{
			ID:   "msg_tooluse",
			Type: "message",
			Role: "assistant",
			Content: []AnthropicContentBlock{
				{Type: "text", Text: "I'll check the weather for you."},
				{Type: "tool_use", ID: "toolu_123", Name: "get_weather", Input: map[string]interface{}{"location": "NYC"}},
			},
			Model:      "claude-sonnet-4-20250514",
			StopReason: "tool_use",
			Usage:      AnthropicUsage{InputTokens: 50, OutputTokens: 80},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-anthropic-tools", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	anthropicReq := AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "What's the weather in NYC?"},
		},
	}
	reqBody, _ := json.Marshal(anthropicReq)

	proxyURL := fmt.Sprintf("http://localhost:%d/v1/messages", port)
	httpReq, _ := http.NewRequest("POST", proxyURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", "test-key")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify response contains tool use
	var anthropicResp AnthropicResponse
	json.NewDecoder(resp.Body).Decode(&anthropicResp)

	if anthropicResp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want %q", anthropicResp.StopReason, "tool_use")
	}
	if len(anthropicResp.Content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(anthropicResp.Content))
	}
	if anthropicResp.Content[1].Type != "tool_use" {
		t.Errorf("Content[1].Type = %q, want %q", anthropicResp.Content[1].Type, "tool_use")
	}
	if anthropicResp.Content[1].Name != "get_weather" {
		t.Errorf("Content[1].Name = %q, want %q", anthropicResp.Content[1].Name, "get_weather")
	}

	captured := waitForRequest(t, 1, 2*time.Second)
	time.Sleep(200 * time.Millisecond)

	if captured.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", captured.InputTokens)
	}
	if captured.OutputTokens != 80 {
		t.Errorf("OutputTokens = %d, want 80", captured.OutputTokens)
	}
}

func TestAnthropicProxyWithSystemPrompt(t *testing.T) {
	resetTestState()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req AnthropicRequest
		json.Unmarshal(body, &req)

		// Verify system prompt was forwarded
		systemText, ok := req.System.(string)
		if !ok || systemText != "You are a helpful assistant." {
			t.Errorf("System prompt not forwarded correctly: %v", req.System)
		}

		resp := AnthropicResponse{
			ID:         "msg_sys",
			Type:       "message",
			Role:       "assistant",
			Content:    []AnthropicContentBlock{{Type: "text", Text: "I'm here to help!"}},
			Model:      "claude-sonnet-4-20250514",
			StopReason: "end_turn",
			Usage:      AnthropicUsage{InputTokens: 30, OutputTokens: 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-anthropic-system", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	anthropicReq := AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    "You are a helpful assistant.",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	reqBody, _ := json.Marshal(anthropicReq)

	proxyURL := fmt.Sprintf("http://localhost:%d/v1/messages", port)
	httpReq, _ := http.NewRequest("POST", proxyURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", "test-key")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	captured := waitForRequest(t, 1, 2*time.Second)
	time.Sleep(200 * time.Millisecond)

	if captured.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", captured.Model, "claude-sonnet-4-20250514")
	}
	if captured.InputTokens != 30 {
		t.Errorf("InputTokens = %d, want 30", captured.InputTokens)
	}
	if captured.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", captured.OutputTokens)
	}
}

func TestAnthropicErrorResponse(t *testing.T) {
	resetTestState()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: field required"}}`))
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-anthropic-error", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	anthropicReq := AnthropicRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	reqBody, _ := json.Marshal(anthropicReq)

	proxyURL := fmt.Sprintf("http://localhost:%d/v1/messages", port)
	httpReq, _ := http.NewRequest("POST", proxyURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", "test-key")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("Expected status 400, got %d", resp.StatusCode)
	}

	captured := waitForRequest(t, 1, 2*time.Second)

	if captured.Status != StatusError {
		t.Errorf("Expected StatusError, got %d", captured.Status)
	}
	if captured.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", captured.StatusCode)
	}
}

// --- Helper function tests ---

func TestExtractAnthropicTextContent(t *testing.T) {
	tests := []struct {
		name    string
		content interface{}
		want    string
	}{
		{"string content", "Hello world", "Hello world"},
		{"empty string", "", ""},
		{"nil", nil, ""},
		{
			"text blocks",
			[]interface{}{
				map[string]interface{}{"type": "text", "text": "Hello"},
				map[string]interface{}{"type": "text", "text": "World"},
			},
			"Hello\nWorld",
		},
		{
			"mixed blocks",
			[]interface{}{
				map[string]interface{}{"type": "text", "text": "I'll help."},
				map[string]interface{}{"type": "tool_use", "id": "toolu_1", "name": "search"},
			},
			"I'll help.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractAnthropicTextContent(tc.content)
			if got != tc.want {
				t.Errorf("extractAnthropicTextContent() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- Streaming Tests ---

func TestExtractTokenUsageFromSSE_Anthropic(t *testing.T) {
	req := &LLMRequest{
		ID:    1,
		Model: "claude-sonnet-4-20250514",
	}

	sseData := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":25,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}

event: message_stop
data: {"type":"message_stop"}
`)

	extractTokenUsageFromSSE(req, sseData)

	if req.InputTokens != 25 {
		t.Errorf("InputTokens = %d, want 25", req.InputTokens)
	}
	if req.OutputTokens != 15 {
		t.Errorf("OutputTokens = %d, want 15", req.OutputTokens)
	}
}

func TestExtractTokenUsageFromSSE_OpenAI(t *testing.T) {
	req := &LLMRequest{
		ID:    1,
		Model: "gpt-4o",
	}

	sseData := []byte(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":18,"completion_tokens":12,"total_tokens":30}}

data: [DONE]
`)

	extractTokenUsageFromSSE(req, sseData)

	if req.InputTokens != 18 {
		t.Errorf("InputTokens = %d, want 18", req.InputTokens)
	}
	if req.OutputTokens != 12 {
		t.Errorf("OutputTokens = %d, want 12", req.OutputTokens)
	}
}

func TestExtractTokenUsage_DetectsSSE(t *testing.T) {
	// Verify that extractTokenUsage correctly detects SSE format and delegates
	req := &LLMRequest{
		ID:    1,
		Model: "claude-sonnet-4-20250514",
	}

	sseData := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","usage":{"input_tokens":42,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":99}}

event: message_stop
data: {"type":"message_stop"}
`)

	extractTokenUsage(req, sseData)

	if req.InputTokens != 42 {
		t.Errorf("InputTokens = %d, want 42", req.InputTokens)
	}
	if req.OutputTokens != 99 {
		t.Errorf("OutputTokens = %d, want 99", req.OutputTokens)
	}
}

func TestAnthropicStreamingProxyIntegration(t *testing.T) {
	resetTestState()

	// Mock Anthropic streaming backend
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":30,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello!\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}

		for _, event := range events {
			fmt.Fprint(w, event)
			flusher.Flush()
		}
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-anthropic-stream", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	anthropicReq := AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	reqBody, _ := json.Marshal(anthropicReq)

	proxyURL := fmt.Sprintf("http://localhost:%d/v1/messages", port)
	httpReq, _ := http.NewRequest("POST", proxyURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", "test-key")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	// Read full body to complete the stream
	io.ReadAll(resp.Body)
	resp.Body.Close()

	captured := waitForRequest(t, 1, 2*time.Second)

	if !captured.IsStreaming {
		t.Error("Expected IsStreaming=true")
	}

	// Wait for async token extraction
	time.Sleep(300 * time.Millisecond)

	if captured.InputTokens != 30 {
		t.Errorf("InputTokens = %d, want 30", captured.InputTokens)
	}
	if captured.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", captured.OutputTokens)
	}
}

// --- Image Tests ---

func TestExtractAnthropicImageURL_Base64(t *testing.T) {
	block := map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type":       "base64",
			"media_type": "image/png",
			"data":       "iVBORw0KGgoAAAANS",
		},
	}

	url, isBase64 := extractAnthropicImageURL(block)
	if !isBase64 {
		t.Error("Expected isBase64=true")
	}
	if url != "data:image/png;base64,iVBORw0KGgoAAAANS" {
		t.Errorf("URL = %q, want data URL", url)
	}
}

func TestExtractAnthropicImageURL_URL(t *testing.T) {
	block := map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type": "url",
			"url":  "https://example.com/image.png",
		},
	}

	url, isBase64 := extractAnthropicImageURL(block)
	if isBase64 {
		t.Error("Expected isBase64=false")
	}
	if url != "https://example.com/image.png" {
		t.Errorf("URL = %q, want https://example.com/image.png", url)
	}
}

func TestAnthropicProxyWithImage(t *testing.T) {
	resetTestState()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req AnthropicRequest
		json.Unmarshal(body, &req)

		// Verify the image was forwarded
		if len(req.Messages) != 1 {
			t.Errorf("Expected 1 message, got %d", len(req.Messages))
		}

		resp := AnthropicResponse{
			ID:         "msg_img",
			Type:       "message",
			Role:       "assistant",
			Content:    []AnthropicContentBlock{{Type: "text", Text: "I see an image of a cat."}},
			Model:      "claude-sonnet-4-20250514",
			StopReason: "end_turn",
			Usage:      AnthropicUsage{InputTokens: 200, OutputTokens: 15},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-anthropic-img", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	// Request with image content block
	anthropicReq := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "What's in this image?"},
					{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": "image/png",
							"data":       "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk",
						},
					},
				},
			},
		},
	}
	reqBody, _ := json.Marshal(anthropicReq)

	proxyURL := fmt.Sprintf("http://localhost:%d/v1/messages", port)
	httpReq, _ := http.NewRequest("POST", proxyURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", "test-key")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	captured := waitForRequest(t, 1, 2*time.Second)

	if captured.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", captured.Model, "claude-sonnet-4-20250514")
	}

	time.Sleep(200 * time.Millisecond)

	if captured.InputTokens != 200 {
		t.Errorf("InputTokens = %d, want 200", captured.InputTokens)
	}
	if captured.OutputTokens != 15 {
		t.Errorf("OutputTokens = %d, want 15", captured.OutputTokens)
	}

	// Verify the request body contains the image data
	if !bytes.Contains(captured.RequestBody, []byte("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk")) {
		t.Error("Request body should contain the base64 image data")
	}
}

func TestNonLLMEndpointPassthrough(t *testing.T) {
	resetTestState()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	if err := StartProxyInstance("test-passthrough", listenAddr, mockServer.URL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	// Non-LLM endpoint should be proxied but not captured
	proxyURL := fmt.Sprintf("http://localhost:%d/v1/models", port)
	resp, err := http.Get(proxyURL)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Should not be captured as an LLM request
	time.Sleep(100 * time.Millisecond)
	requestsMu.RLock()
	count := len(requests)
	requestsMu.RUnlock()

	if count != 0 {
		t.Errorf("Expected 0 captured requests for non-LLM endpoint, got %d", count)
	}
}

// --- responseRecorder Tests ---

func TestResponseRecorderImplementsFlusher(t *testing.T) {
	// The responseRecorder must implement http.Flusher for SSE streaming to work.
	// Without this, httputil.ReverseProxy can't flush SSE chunks to the client.
	w := httptest.NewRecorder()
	rec := newResponseRecorder(w)

	// Cast to http.ResponseWriter interface first, then check for http.Flusher
	// This mirrors what httputil.ReverseProxy does internally
	var rw http.ResponseWriter = rec
	flusher, ok := rw.(http.Flusher)
	if !ok {
		t.Fatal("responseRecorder does not implement http.Flusher - SSE streaming will break")
	}

	// Should not panic
	flusher.Flush()
}

// --- ErrorHandler Tests ---

func TestProxyErrorHandler_Returns502(t *testing.T) {
	resetTestState()

	// Start proxy pointing at an unreachable host
	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)

	err := StartProxyInstance("test", listenAddr, "http://127.0.0.1:1") // port 1 is unreachable
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Send a request that will fail at the proxy level
	reqBody := `{"model":"test","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/v1/chat/completions", port),
		"application/json",
		bytes.NewBufferString(reqBody),
	)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should get 502 Bad Gateway, not 200
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("Expected status 502 for unreachable upstream, got %d", resp.StatusCode)
	}

	// Wait for request to be finalized
	req := waitForRequest(t, 1, 5*time.Second)
	if req.Status != StatusError {
		t.Errorf("Expected StatusError for proxy error, got %v", req.Status)
	}
	if req.StatusCode != 502 {
		t.Errorf("Expected status code 502, got %d", req.StatusCode)
	}
}

// --- Base64 Truncation Tests ---

func TestTruncateLongBase64Strings(t *testing.T) {
	// Anthropic format: raw base64 in "data" field
	longBase64 := strings.Repeat("iVBORw0KGgoAAAANSUhEUg", 100) // ~2200 chars
	anthropicJSON := fmt.Sprintf(`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"%s"}}`, longBase64)

	result := truncateLongBase64Strings([]byte(anthropicJSON))
	resultStr := string(result)

	if len(resultStr) >= len(anthropicJSON) {
		t.Errorf("Expected truncated output to be shorter than input (%d >= %d)", len(resultStr), len(anthropicJSON))
	}
	if !strings.Contains(resultStr, "chars truncated") {
		t.Error("Expected truncation placeholder in output")
	}

	// Should still be valid-ish JSON structure (has the key names)
	if !strings.Contains(resultStr, `"type"`) {
		t.Error("Expected JSON structure to be preserved")
	}
}

func TestTruncateLongBase64Strings_DataURL(t *testing.T) {
	// OpenAI format: data URL
	longDataURL := "data:image/png;base64," + strings.Repeat("AAAA", 500)
	openaiJSON := fmt.Sprintf(`{"image_url":{"url":"%s"}}`, longDataURL)

	result := truncateLongBase64Strings([]byte(openaiJSON))
	resultStr := string(result)

	if len(resultStr) >= len(openaiJSON) {
		t.Errorf("Expected truncated output to be shorter")
	}
	if !strings.Contains(resultStr, "chars truncated") {
		t.Error("Expected truncation placeholder")
	}
}

func TestTruncateLongBase64Strings_PreservesNormalText(t *testing.T) {
	// Normal text with spaces should NOT be truncated even if long
	longText := strings.Repeat("Hello world, this is a normal message. ", 50)
	normalJSON := fmt.Sprintf(`{"role":"user","content":"%s"}`, longText)

	result := truncateLongBase64Strings([]byte(normalJSON))

	if string(result) != normalJSON {
		t.Error("Normal text should not be truncated")
	}
}

func TestTruncateLongBase64Strings_ShortBase64Preserved(t *testing.T) {
	// Short base64 strings should NOT be truncated
	shortJSON := `{"data":"SGVsbG8gV29ybGQ="}`

	result := truncateLongBase64Strings([]byte(shortJSON))

	if string(result) != shortJSON {
		t.Error("Short base64 should not be truncated")
	}
}

// --- Anthropic Thinking Block Tests ---

func TestAnthropicProxyWithThinking(t *testing.T) {
	resetTestState()

	// Mock Anthropic server that returns thinking blocks
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request was forwarded correctly
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		// Verify thinking config is forwarded
		thinking, ok := req["thinking"].(map[string]interface{})
		if !ok {
			t.Error("Expected thinking config to be forwarded")
		} else {
			if thinkType, _ := thinking["type"].(string); thinkType != "enabled" {
				t.Errorf("Expected thinking type 'enabled', got %q", thinkType)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		resp := map[string]interface{}{
			"id":    "msg_thinking",
			"type":  "message",
			"role":  "assistant",
			"model": "claude-opus-4-6",
			"content": []map[string]interface{}{
				{
					"type":      "thinking",
					"thinking":  "Let me analyze this step by step...",
					"signature": "abc123",
				},
				{
					"type": "text",
					"text": "Here is my answer.",
				},
			},
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":  100,
				"output_tokens": 50,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	port := getFreePort(t)
	listenAddr := fmt.Sprintf(":%d", port)
	StartProxyInstance("test", listenAddr, mockServer.URL)
	time.Sleep(100 * time.Millisecond)

	// Send request with thinking enabled (matches paperboy's anthropic.ts)
	reqBody := `{
		"model": "claude-opus-4-6",
		"messages": [{"role": "user", "content": "Think about this"}],
		"max_tokens": 16000,
		"thinking": {"type": "enabled", "budget_tokens": 10000},
		"stream": false
	}`

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/v1/messages", port),
		"application/json",
		bytes.NewBufferString(reqBody),
	)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify response body was forwarded correctly
	respBody, _ := io.ReadAll(resp.Body)
	var respData AnthropicResponse
	if err := json.Unmarshal(respBody, &respData); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check thinking block is preserved
	if len(respData.Content) < 2 {
		t.Fatalf("Expected at least 2 content blocks, got %d", len(respData.Content))
	}
	if respData.Content[0].Type != "thinking" {
		t.Errorf("Expected first block type 'thinking', got %q", respData.Content[0].Type)
	}
	if respData.Content[0].Thinking != "Let me analyze this step by step..." {
		t.Errorf("Expected thinking content to be preserved, got %q", respData.Content[0].Thinking)
	}
	if respData.Content[1].Type != "text" {
		t.Errorf("Expected second block type 'text', got %q", respData.Content[1].Type)
	}

	// Verify proxy captured the request correctly
	req := waitForRequest(t, 1, 5*time.Second)
	if req.Status != StatusComplete {
		t.Errorf("Expected StatusComplete, got %v", req.Status)
	}
}
