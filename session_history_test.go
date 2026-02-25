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

func TestRunInspectCommand_Filters(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())

	history, err := NewSessionHistory("sess-filters", ":8080", "https://api.openai.com")
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
		StartTime:   time.Now().Add(-2 * time.Second),
		RequestBody: []byte(`{"messages":[{"role":"user","content":"alpha hello"}]}`),
	})
	history.UpsertRequest(&LLMRequest{
		ID:          2,
		Method:      "POST",
		Path:        "/v1/messages",
		Model:       "claude-3-5-sonnet",
		Status:      StatusError,
		StatusCode:  429,
		StartTime:   time.Now().Add(-time.Second),
		RequestBody: []byte(`{"messages":[{"role":"user","content":"beta"}]}`),
	})
	history.UpsertRequest(&LLMRequest{
		ID:           3,
		Method:       "POST",
		Path:         "/v1/chat/completions",
		Model:        "gpt-4o",
		Status:       StatusComplete,
		StatusCode:   200,
		StartTime:    time.Now(),
		RequestBody:  []byte(`{"messages":[{"role":"user","content":"gamma"}]}`),
		ResponseBody: []byte(`{"choices":[{"message":{"content":"hello gamma"}}]}`),
	})

	var out bytes.Buffer
	err = RunInspectCommand(&out, InspectOptions{
		SessionID: "sess-filters",
		JSON:      true,
		Model:     "gpt",
		Path:      "/chat/completions",
		Status:    "complete",
		Code:      200,
		Search:    "gamma",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("RunInspectCommand filter error: %v", err)
	}

	var payload struct {
		MatchedRequests int                     `json:"matched_requests"`
		ShownRequests   int                     `json:"shown_requests"`
		Requests        []SessionHistoryRequest `json:"requests"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal inspect output: %v", err)
	}

	if payload.MatchedRequests != 1 || payload.ShownRequests != 1 {
		t.Fatalf("expected 1 matched/shown request, got matched=%d shown=%d", payload.MatchedRequests, payload.ShownRequests)
	}
	if len(payload.Requests) != 1 || payload.Requests[0].ID != 3 {
		t.Fatalf("expected only request #3 after filters, got %+v", payload.Requests)
	}
}

func TestRunInspectCommand_FilterInvalidStatus(t *testing.T) {
	t.Setenv(sessionHistoryDirEnv, t.TempDir())

	history, err := NewSessionHistory("sess-invalid-status", ":8080", "https://api.openai.com")
	if err != nil {
		t.Fatalf("NewSessionHistory error: %v", err)
	}
	history.UpsertRequest(&LLMRequest{
		ID:         1,
		Method:     "POST",
		Path:       "/v1/chat/completions",
		Model:      "gpt-4o",
		Status:     StatusComplete,
		StatusCode: 200,
		StartTime:  time.Now(),
	})

	var out bytes.Buffer
	err = RunInspectCommand(&out, InspectOptions{
		SessionID: "sess-invalid-status",
		Status:    "wat",
	})
	if err == nil {
		t.Fatal("expected invalid status filter to return an error")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}
