package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionHistoryRoundTrip(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())

	history, err := NewSessionHistory("sess-roundtrip", ":8080", "https://api.openai.com")
	if err != nil {
		t.Fatalf("NewSessionHistory error: %v", err)
	}

	req := &LLMRequest{
		ID:             1,
		Method:         "POST",
		Path:           "/v1/chat/completions",
		URL:            "https://api.openai.com/v1/chat/completions",
		Model:          "gpt-4o",
		Status:         StatusPending,
		StartTime:      time.Now().Add(-2 * time.Second),
		RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RequestBody:    []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`),
		RequestSize:    64,
	}
	history.UpsertRequest(req)

	req.Status = StatusComplete
	req.StatusCode = 200
	req.Duration = 250 * time.Millisecond
	req.ResponseHeaders = map[string][]string{"Content-Type": {"application/json"}}
	req.ResponseBody = []byte(`{"id":"chatcmpl_123","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	req.ResponseSize = len(req.ResponseBody)
	req.InputTokens = 10
	req.OutputTokens = 5
	history.UpsertRequest(req)

	snapshot, err := LoadSessionHistory("sess-roundtrip")
	if err != nil {
		t.Fatalf("LoadSessionHistory error: %v", err)
	}

	if snapshot.SessionID != "sess-roundtrip" {
		t.Fatalf("snapshot.SessionID = %q, want sess-roundtrip", snapshot.SessionID)
	}
	if snapshot.RequestCount != 1 {
		t.Fatalf("snapshot.RequestCount = %d, want 1", snapshot.RequestCount)
	}
	if len(snapshot.Requests) != 1 {
		t.Fatalf("len(snapshot.Requests) = %d, want 1", len(snapshot.Requests))
	}

	got := snapshot.Requests[0]
	if got.StatusText != "complete" {
		t.Errorf("StatusText = %q, want complete", got.StatusText)
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if got.InputTokens != 10 || got.OutputTokens != 5 {
		t.Errorf("tokens = %d/%d, want 10/5", got.InputTokens, got.OutputTokens)
	}
	if !strings.Contains(got.RequestBody, `"messages"`) {
		t.Errorf("request body missing expected content: %q", got.RequestBody)
	}
}

func TestSessionHistoryTrimsOldRequests(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())

	history, err := NewSessionHistory("sess-trim", ":8080", "https://api.openai.com")
	if err != nil {
		t.Fatalf("NewSessionHistory error: %v", err)
	}
	history.maxRequests = 2

	for i := 1; i <= 3; i++ {
		history.UpsertRequest(&LLMRequest{
			ID:         i,
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      "gpt-4o",
			Status:     StatusComplete,
			StatusCode: 200,
			StartTime:  time.Now(),
		})
	}

	snapshot, err := LoadSessionHistory("sess-trim")
	if err != nil {
		t.Fatalf("LoadSessionHistory error: %v", err)
	}

	if len(snapshot.Requests) != 2 {
		t.Fatalf("len(snapshot.Requests) = %d, want 2", len(snapshot.Requests))
	}
	if snapshot.Requests[0].ID != 2 || snapshot.Requests[1].ID != 3 {
		t.Fatalf("trimmed IDs = [%d,%d], want [2,3]", snapshot.Requests[0].ID, snapshot.Requests[1].ID)
	}
}

func TestRunInspectCommand(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())

	history, err := NewSessionHistory("sess-inspect", ":8080", "https://api.openai.com")
	if err != nil {
		t.Fatalf("NewSessionHistory error: %v", err)
	}

	history.UpsertRequest(&LLMRequest{
		ID:          1,
		Method:      "POST",
		Path:        "/v1/chat/completions",
		Model:       "gpt-4o-mini",
		Status:      StatusComplete,
		StatusCode:  200,
		StartTime:   time.Now().Add(-time.Second),
		Duration:    80 * time.Millisecond,
		RequestBody: []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	})
	history.UpsertRequest(&LLMRequest{
		ID:           2,
		Method:       "POST",
		Path:         "/v1/chat/completions",
		Model:        "gpt-4o",
		Status:       StatusError,
		StatusCode:   429,
		StartTime:    time.Now(),
		RequestBody:  []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseBody: []byte(`{"error":"rate limit"}`),
	})

	var jsonOut bytes.Buffer
	if err := RunInspectCommand(&jsonOut, InspectOptions{
		SessionID: "sess-inspect",
		Limit:     1,
		JSON:      true,
	}); err != nil {
		t.Fatalf("RunInspectCommand JSON error: %v", err)
	}

	var payload struct {
		ShownRequests int                     `json:"shown_requests"`
		Requests      []SessionHistoryRequest `json:"requests"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal inspect output: %v", err)
	}
	if payload.ShownRequests != 1 {
		t.Fatalf("ShownRequests = %d, want 1", payload.ShownRequests)
	}
	if len(payload.Requests) != 1 || payload.Requests[0].ID != 2 {
		t.Fatalf("inspect JSON requests = %+v, want only request #2", payload.Requests)
	}

	var detailOut bytes.Buffer
	if err := RunInspectCommand(&detailOut, InspectOptions{
		SessionID: "sess-inspect",
		RequestID: 2,
	}); err != nil {
		t.Fatalf("RunInspectCommand detail error: %v", err)
	}

	detail := detailOut.String()
	if !strings.Contains(detail, "Request:   #2") {
		t.Fatalf("detail output missing request id: %s", detail)
	}
	if !strings.Contains(detail, `"rate limit"`) {
		t.Fatalf("detail output missing response body: %s", detail)
	}
}
