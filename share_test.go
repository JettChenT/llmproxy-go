package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func seedShareHistory(t *testing.T, sessionID string) {
	t.Helper()

	history, err := NewSessionHistory(sessionID, ":8080", "https://api.openai.com")
	if err != nil {
		t.Fatalf("NewSessionHistory error: %v", err)
	}

	history.UpsertRequest(&LLMRequest{
		ID:          1,
		Method:      "POST",
		Path:        "/v1/chat/completions",
		URL:         "https://api.openai.com/v1/chat/completions",
		Model:       "gpt-4o-mini",
		Status:      StatusComplete,
		StatusCode:  200,
		StartTime:   time.Now().Add(-2 * time.Second),
		Duration:    120 * time.Millisecond,
		RequestBody: []byte(`{"messages":[{"role":"user","content":"alpha"}]}`),
		ResponseBody: []byte(
			`{"choices":[{"message":{"role":"assistant","content":"alpha ok"}}]}`,
		),
	})
	history.UpsertRequest(&LLMRequest{
		ID:          2,
		Method:      "POST",
		Path:        "/v1/chat/completions",
		URL:         "https://api.openai.com/v1/chat/completions",
		Model:       "gpt-4o",
		Status:      StatusError,
		StatusCode:  429,
		StartTime:   time.Now().Add(-time.Second),
		RequestBody: []byte(`{"messages":[{"role":"user","content":"rate limit test"}]}`),
		ResponseBody: []byte(
			`{"error":{"message":"rate limit"}}`,
		),
	})
	history.UpsertRequest(&LLMRequest{
		ID:          3,
		Method:      "POST",
		Path:        "/v1/messages",
		URL:         "https://api.anthropic.com/v1/messages",
		Model:       "claude-3-5-sonnet",
		Status:      StatusComplete,
		StatusCode:  200,
		StartTime:   time.Now(),
		RequestBody: []byte(`{"messages":[{"role":"user","content":"beta"}]}`),
		ResponseBody: []byte(
			`{"content":[{"type":"text","text":"beta ok"}]}`,
		),
	})
}

func TestRunShareCommand_RequestID(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())
	seedShareHistory(t, "sess-share-request")

	var gotPayload shareUploadPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/shares" {
			t.Errorf("expected /api/shares path, got %s", r.URL.Path)
		}
		if r.Header.Get("X-LLMProxy-Api-Key") != "secret-token" {
			t.Errorf("expected API key header")
		}

		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"share_uuid":    "123e4567-e89b-12d3-a456-426614174000",
			"share_url":     "http://example.test/share/123e4567-e89b-12d3-a456-426614174000",
			"request_count": 1,
			"created_at":    "2026-02-26T00:00:00Z",
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	err := RunShareCommand(&out, ShareOptions{
		SessionID:   "sess-share-request",
		RequestID:   2,
		PlatformURL: server.URL,
		APIKey:      "secret-token",
		Title:       "Rate limit repro",
	})
	if err != nil {
		t.Fatalf("RunShareCommand error: %v", err)
	}

	if gotPayload.Title != "Rate limit repro" {
		t.Fatalf("payload title = %q, want %q", gotPayload.Title, "Rate limit repro")
	}
	if gotPayload.Source.SessionID != "sess-share-request" {
		t.Fatalf("payload session = %q, want sess-share-request", gotPayload.Source.SessionID)
	}
	if len(gotPayload.Requests) != 1 || gotPayload.Requests[0].ID != 2 {
		t.Fatalf("expected request #2 only, got %+v", gotPayload.Requests)
	}

	output := out.String()
	if !strings.Contains(output, "Shared 1 request(s)") {
		t.Fatalf("expected summary output, got: %s", output)
	}
	if !strings.Contains(output, "/share/123e4567-e89b-12d3-a456-426614174000") {
		t.Fatalf("expected share URL output, got: %s", output)
	}
}

func TestRunShareCommand_FiltersAndLimit(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())
	seedShareHistory(t, "sess-share-filters")

	var gotPayload shareUploadPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"share_uuid":    "123e4567-e89b-12d3-a456-426614174111",
			"share_url":     "http://example.test/share/123e4567-e89b-12d3-a456-426614174111",
			"request_count": 1,
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	err := RunShareCommand(&out, ShareOptions{
		SessionID:   "sess-share-filters",
		PlatformURL: server.URL,
		Model:       "gpt",
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("RunShareCommand error: %v", err)
	}

	if len(gotPayload.Requests) != 1 {
		t.Fatalf("expected one request in payload, got %d", len(gotPayload.Requests))
	}
	// Matching gpt requests are IDs 1 and 2; limit=1 should pick most recent = ID 2.
	if gotPayload.Requests[0].ID != 2 {
		t.Fatalf("expected latest matched request ID 2, got %d", gotPayload.Requests[0].ID)
	}
}

func TestRunShareCommand_APIReturnsError(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())
	seedShareHistory(t, "sess-share-error")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "upstream storage unavailable",
		})
	}))
	defer server.Close()

	err := RunShareCommand(io.Discard, ShareOptions{
		SessionID:   "sess-share-error",
		RequestID:   1,
		PlatformURL: server.URL,
	})
	if err == nil {
		t.Fatal("expected RunShareCommand to return an error")
	}
	if !strings.Contains(err.Error(), "upstream storage unavailable") {
		t.Fatalf("expected API error details, got: %v", err)
	}
}
