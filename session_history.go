package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionHistoryRequestLimit = 200
	maxSessionHistoryBodyBytes        = 64 * 1024
	sessionHistoryDirEnv              = "LLMPROXY_SESSION_DIR"
)

var activeSessionHistory *SessionHistory

// SessionHistoryRequest is a serialized request record used by the inspect command.
type SessionHistoryRequest struct {
	ID                    int                 `json:"id"`
	Method                string              `json:"method"`
	Path                  string              `json:"path"`
	URL                   string              `json:"url"`
	Model                 string              `json:"model"`
	Status                RequestStatus       `json:"status"`
	StatusText            string              `json:"status_text"`
	StatusCode            int                 `json:"status_code"`
	StartTime             time.Time           `json:"start_time"`
	DurationMs            int64               `json:"duration_ms"`
	TTFTMs                int64               `json:"ttft_ms"`
	RequestHeaders        map[string][]string `json:"request_headers,omitempty"`
	ResponseHeaders       map[string][]string `json:"response_headers,omitempty"`
	RequestBody           string              `json:"request_body,omitempty"`
	ResponseBody          string              `json:"response_body,omitempty"`
	RequestBodyTruncated  bool                `json:"request_body_truncated,omitempty"`
	ResponseBodyTruncated bool                `json:"response_body_truncated,omitempty"`
	RequestSize           int                 `json:"request_size"`
	ResponseSize          int                 `json:"response_size"`
	IsStreaming           bool                `json:"is_streaming"`
	CachedResponse        bool                `json:"cached_response"`
	ProxyName             string              `json:"proxy_name,omitempty"`
	ProxyListen           string              `json:"proxy_listen,omitempty"`
	EstimatedInputTokens  int                 `json:"estimated_input_tokens"`
	InputTokens           int                 `json:"input_tokens"`
	OutputTokens          int                 `json:"output_tokens"`
	ProviderID            string              `json:"provider_id,omitempty"`
	Cost                  float64             `json:"cost"`
}

// SessionHistorySnapshot is the persisted shape used by llmproxy-go inspect.
type SessionHistorySnapshot struct {
	SessionID    string                  `json:"session_id"`
	ListenAddr   string                  `json:"listen_addr"`
	TargetURL    string                  `json:"target_url"`
	PID          int                     `json:"pid"`
	StartedAt    time.Time               `json:"started_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
	RequestCount int                     `json:"request_count"`
	Requests     []SessionHistoryRequest `json:"requests"`
}

// SessionHistory tracks request history for one running proxy session.
type SessionHistory struct {
	mu          sync.Mutex
	sessionID   string
	listenAddr  string
	targetURL   string
	startedAt   time.Time
	filePath    string
	maxRequests int
	order       []int
	requests    map[int]SessionHistoryRequest
}

// GenerateSessionID creates a short, copy-friendly session ID.
func GenerateSessionID() string {
	randomBytes := make([]byte, 6)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return "sess-" + hex.EncodeToString(randomBytes)
}

// StartSessionHistory initializes a new runtime session history store.
func StartSessionHistory(listenAddr, targetURL string) (string, error) {
	sessionID := GenerateSessionID()
	history, err := NewSessionHistory(sessionID, listenAddr, targetURL)
	if err != nil {
		return "", err
	}
	activeSessionHistory = history
	return sessionID, nil
}

// StopSessionHistory detaches the runtime session history store.
func StopSessionHistory() {
	activeSessionHistory = nil
}

// RecordSessionRequest writes/updates one request in the active session history.
func RecordSessionRequest(req *LLMRequest) {
	if activeSessionHistory == nil || req == nil {
		return
	}
	activeSessionHistory.UpsertRequest(req)
}

// NewSessionHistory creates a new persisted session history file.
func NewSessionHistory(sessionID, listenAddr, targetURL string) (*SessionHistory, error) {
	filePath, err := sessionHistoryFilePath(sessionID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create session history directory: %w", err)
	}

	h := &SessionHistory{
		sessionID:   sessionID,
		listenAddr:  listenAddr,
		targetURL:   targetURL,
		startedAt:   time.Now(),
		filePath:    filePath,
		maxRequests: defaultSessionHistoryRequestLimit,
		order:       make([]int, 0),
		requests:    make(map[int]SessionHistoryRequest),
	}

	if err := h.persistLocked(); err != nil {
		return nil, err
	}
	return h, nil
}

// UpsertRequest inserts or updates a request in the session history.
func (h *SessionHistory) UpsertRequest(req *LLMRequest) {
	if h == nil || req == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.requests[req.ID]; !exists {
		h.order = append(h.order, req.ID)
	}
	h.requests[req.ID] = toSessionHistoryRequest(req)

	for len(h.order) > h.maxRequests {
		oldestID := h.order[0]
		h.order = h.order[1:]
		delete(h.requests, oldestID)
	}

	_ = h.persistLocked()
}

func (h *SessionHistory) persistLocked() error {
	requests := make([]SessionHistoryRequest, 0, len(h.order))
	for _, id := range h.order {
		if req, ok := h.requests[id]; ok {
			requests = append(requests, req)
		}
	}

	snapshot := SessionHistorySnapshot{
		SessionID:    h.sessionID,
		ListenAddr:   h.listenAddr,
		TargetURL:    h.targetURL,
		PID:          os.Getpid(),
		StartedAt:    h.startedAt,
		UpdatedAt:    time.Now(),
		RequestCount: len(requests),
		Requests:     requests,
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session history: %w", err)
	}

	tempPath := h.filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write session history temp file: %w", err)
	}
	if err := os.Rename(tempPath, h.filePath); err != nil {
		return fmt.Errorf("failed to commit session history file: %w", err)
	}
	return nil
}

func toSessionHistoryRequest(req *LLMRequest) SessionHistoryRequest {
	requestBody, requestBodyTruncated := truncateBodyForHistory(req.RequestBody)
	responseBody, responseBodyTruncated := truncateBodyForHistory(req.ResponseBody)

	return SessionHistoryRequest{
		ID:                    req.ID,
		Method:                req.Method,
		Path:                  req.Path,
		URL:                   req.URL,
		Model:                 req.Model,
		Status:                req.Status,
		StatusText:            requestStatusText(req.Status),
		StatusCode:            req.StatusCode,
		StartTime:             req.StartTime,
		DurationMs:            req.Duration.Milliseconds(),
		TTFTMs:                req.TTFT.Milliseconds(),
		RequestHeaders:        cloneStringSliceMap(req.RequestHeaders),
		ResponseHeaders:       cloneStringSliceMap(req.ResponseHeaders),
		RequestBody:           requestBody,
		ResponseBody:          responseBody,
		RequestBodyTruncated:  requestBodyTruncated,
		ResponseBodyTruncated: responseBodyTruncated,
		RequestSize:           req.RequestSize,
		ResponseSize:          req.ResponseSize,
		IsStreaming:           req.IsStreaming,
		CachedResponse:        req.CachedResponse,
		ProxyName:             req.ProxyName,
		ProxyListen:           req.ProxyListen,
		EstimatedInputTokens:  req.EstimatedInputTokens,
		InputTokens:           req.InputTokens,
		OutputTokens:          req.OutputTokens,
		ProviderID:            req.ProviderID,
		Cost:                  req.Cost,
	}
}

func requestStatusText(status RequestStatus) string {
	switch status {
	case StatusPending:
		return "pending"
	case StatusComplete:
		return "complete"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

func truncateBodyForHistory(body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	if len(body) <= maxSessionHistoryBodyBytes {
		return string(body), false
	}
	extra := len(body) - maxSessionHistoryBodyBytes
	truncated := string(body[:maxSessionHistoryBodyBytes]) + fmt.Sprintf("\n\n... [truncated %d bytes]", extra)
	return truncated, true
}

func cloneStringSliceMap(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for k, values := range src {
		copied := make([]string, len(values))
		copy(copied, values)
		out[k] = copied
	}
	return out
}

// LoadSessionHistory reads a persisted session history snapshot by session ID.
func LoadSessionHistory(sessionID string) (*SessionHistorySnapshot, error) {
	filePath, err := sessionHistoryFilePath(sessionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session history: %w", err)
	}

	var snapshot SessionHistorySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to parse session history: %w", err)
	}
	return &snapshot, nil
}

// RecentRequests returns the most recent N requests from the snapshot.
func (s *SessionHistorySnapshot) RecentRequests(limit int) []SessionHistoryRequest {
	if s == nil {
		return nil
	}
	if limit <= 0 || limit >= len(s.Requests) {
		out := make([]SessionHistoryRequest, len(s.Requests))
		copy(out, s.Requests)
		return out
	}

	start := len(s.Requests) - limit
	out := make([]SessionHistoryRequest, limit)
	copy(out, s.Requests[start:])
	return out
}

// FindRequest finds one request by ID from the snapshot.
func (s *SessionHistorySnapshot) FindRequest(id int) (SessionHistoryRequest, bool) {
	if s == nil {
		return SessionHistoryRequest{}, false
	}
	for _, req := range s.Requests {
		if req.ID == id {
			return req, true
		}
	}
	return SessionHistoryRequest{}, false
}

func sessionHistoryFilePath(sessionID string) (string, error) {
	if !isSafeSessionID(sessionID) {
		return "", fmt.Errorf("invalid session ID %q", sessionID)
	}
	return filepath.Join(sessionHistoryDirectory(), sessionID+".json"), nil
}

func sessionHistoryDirectory() string {
	if dir := strings.TrimSpace(os.Getenv(sessionHistoryDirEnv)); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "llmproxy-go", "sessions")
	}
	return filepath.Join(home, ".llmproxy-go", "sessions")
}

func isSafeSessionID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	for _, r := range sessionID {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
