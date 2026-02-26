package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ShareOptions struct {
	SessionID   string
	RequestID   int
	Limit       int
	JSON        bool
	Search      string
	Model       string
	Path        string
	Status      string
	Code        int
	PlatformURL string
	APIKey      string
	Title       string
}

type shareSource struct {
	SessionID  string `json:"session_id"`
	ListenAddr string `json:"listen_addr"`
	TargetURL  string `json:"target_url"`
}

type shareUploadPayload struct {
	Title    string                  `json:"title,omitempty"`
	Source   shareSource             `json:"source"`
	Requests []SessionHistoryRequest `json:"requests"`
}

type shareUploadResponse struct {
	ShareUUID    string `json:"share_uuid"`
	ShareURL     string `json:"share_url"`
	RequestCount int    `json:"request_count"`
	CreatedAt    string `json:"created_at"`
}

// RunShareCommand uploads one or more session requests to the web share platform.
func RunShareCommand(out io.Writer, opts ShareOptions) error {
	sessionID := strings.TrimSpace(opts.SessionID)
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}

	snapshot, err := LoadSessionHistory(sessionID)
	if err != nil {
		return err
	}

	requestsToShare, err := selectRequestsForShare(snapshot, opts)
	if err != nil {
		return err
	}

	endpoint, err := buildShareEndpoint(opts.PlatformURL)
	if err != nil {
		return err
	}

	payload := shareUploadPayload{
		Title: strings.TrimSpace(opts.Title),
		Source: shareSource{
			SessionID:  snapshot.SessionID,
			ListenAddr: snapshot.ListenAddr,
			TargetURL:  snapshot.TargetURL,
		},
		Requests: requestsToShare,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize upload payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := strings.TrimSpace(opts.APIKey); apiKey != "" {
		req.Header.Set("X-LLMProxy-Api-Key", apiKey)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("share upload failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read share response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseShareUploadError(resp.StatusCode, respBody)
	}

	var parsed shareUploadResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("failed to parse share response: %w", err)
	}
	if parsed.ShareURL == "" && parsed.ShareUUID != "" {
		parsed.ShareURL = buildShareURL(opts.PlatformURL, parsed.ShareUUID)
	}
	if parsed.ShareURL == "" {
		return fmt.Errorf("share response did not include share_url")
	}

	if opts.JSON {
		result := struct {
			SessionID    string `json:"session_id"`
			RequestIDs   []int  `json:"request_ids"`
			RequestCount int    `json:"request_count"`
			ShareUUID    string `json:"share_uuid"`
			ShareURL     string `json:"share_url"`
			CreatedAt    string `json:"created_at,omitempty"`
		}{
			SessionID:    snapshot.SessionID,
			RequestIDs:   collectRequestIDs(requestsToShare),
			RequestCount: len(requestsToShare),
			ShareUUID:    parsed.ShareUUID,
			ShareURL:     parsed.ShareURL,
			CreatedAt:    parsed.CreatedAt,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(
		out,
		"Shared %d request(s) from session %s\n%s\n",
		len(requestsToShare),
		snapshot.SessionID,
		parsed.ShareURL,
	)
	return nil
}

func selectRequestsForShare(snapshot *SessionHistorySnapshot, opts ShareOptions) ([]SessionHistoryRequest, error) {
	if opts.RequestID > 0 {
		request, ok := snapshot.FindRequest(opts.RequestID)
		if !ok {
			return nil, fmt.Errorf("request %d not found in session %s", opts.RequestID, opts.SessionID)
		}
		return []SessionHistoryRequest{request}, nil
	}

	filtered, err := filterSessionRequests(snapshot.Requests, InspectOptions{
		Search: opts.Search,
		Model:  opts.Model,
		Path:   opts.Path,
		Status: opts.Status,
		Code:   opts.Code,
	})
	if err != nil {
		return nil, err
	}

	selected := limitRecentRequests(filtered, opts.Limit)
	if len(selected) == 0 {
		return nil, fmt.Errorf("no requests matched share filters")
	}
	return selected, nil
}

func buildShareEndpoint(platformURL string) (string, error) {
	raw := strings.TrimSpace(platformURL)
	if raw == "" {
		return "", fmt.Errorf("share platform URL is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid share platform URL %q: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("share platform URL must include scheme and host")
	}

	path := strings.TrimSuffix(u.Path, "/")
	u.Path = path + "/api/shares"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func buildShareURL(platformURL, shareUUID string) string {
	raw := strings.TrimSpace(platformURL)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}

	path := strings.TrimSuffix(u.Path, "/")
	u.Path = path + "/share/" + shareUUID
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func parseShareUploadError(statusCode int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return fmt.Errorf("share API returned %d: %s", statusCode, payload.Error)
	}

	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = http.StatusText(statusCode)
	}
	return fmt.Errorf("share API returned %d: %s", statusCode, detail)
}

func collectRequestIDs(requests []SessionHistoryRequest) []int {
	ids := make([]int, 0, len(requests))
	for _, req := range requests {
		ids = append(ids, req.ID)
	}
	return ids
}
