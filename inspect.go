package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type InspectOptions struct {
	SessionID string
	Limit     int
	RequestID int
	JSON      bool
}

// RunInspectCommand prints recent request history for a session.
func RunInspectCommand(out io.Writer, opts InspectOptions) error {
	if strings.TrimSpace(opts.SessionID) == "" {
		return fmt.Errorf("session ID is required")
	}

	snapshot, err := LoadSessionHistory(opts.SessionID)
	if err != nil {
		return err
	}

	if opts.RequestID > 0 {
		req, ok := snapshot.FindRequest(opts.RequestID)
		if !ok {
			return fmt.Errorf("request %d not found in session %s", opts.RequestID, opts.SessionID)
		}
		if opts.JSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(req)
		}
		renderRequestDetail(out, snapshot, req)
		return nil
	}

	recent := snapshot.RecentRequests(opts.Limit)
	if opts.JSON {
		payload := struct {
			SessionID     string                  `json:"session_id"`
			ListenAddr    string                  `json:"listen_addr"`
			TargetURL     string                  `json:"target_url"`
			PID           int                     `json:"pid"`
			StartedAt     time.Time               `json:"started_at"`
			UpdatedAt     time.Time               `json:"updated_at"`
			TotalRequests int                     `json:"total_requests"`
			ShownRequests int                     `json:"shown_requests"`
			Requests      []SessionHistoryRequest `json:"requests"`
		}{
			SessionID:     snapshot.SessionID,
			ListenAddr:    snapshot.ListenAddr,
			TargetURL:     snapshot.TargetURL,
			PID:           snapshot.PID,
			StartedAt:     snapshot.StartedAt,
			UpdatedAt:     snapshot.UpdatedAt,
			TotalRequests: snapshot.RequestCount,
			ShownRequests: len(recent),
			Requests:      recent,
		}

		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	renderSummaryTable(out, snapshot, recent)
	return nil
}

func renderSummaryTable(out io.Writer, snapshot *SessionHistorySnapshot, requests []SessionHistoryRequest) {
	age := time.Since(snapshot.UpdatedAt).Round(time.Second)
	if age < 0 {
		age = 0
	}
	fmt.Fprintf(out, "Session: %s\n", snapshot.SessionID)
	fmt.Fprintf(out, "Proxy:   %s -> %s\n", snapshot.ListenAddr, snapshot.TargetURL)
	fmt.Fprintf(out, "PID:     %d\n", snapshot.PID)
	fmt.Fprintf(out, "Updated: %s ago\n", age)
	fmt.Fprintf(out, "Showing %d of %d request(s)\n\n", len(requests), snapshot.RequestCount)

	if len(requests) == 0 {
		fmt.Fprintln(out, "(no requests yet)")
		return
	}

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tCODE\tMODEL\tPATH\tDURATION\tTOKENS\tCOST\tPROXY")
	for _, req := range requests {
		duration := "-"
		if req.DurationMs > 0 {
			duration = formatDuration(time.Duration(req.DurationMs) * time.Millisecond)
		}

		tokens := "-"
		if req.InputTokens > 0 || req.OutputTokens > 0 {
			tokens = fmt.Sprintf("%d/%d", req.InputTokens, req.OutputTokens)
		} else if req.EstimatedInputTokens > 0 {
			tokens = fmt.Sprintf("~%d/-", req.EstimatedInputTokens)
		}

		code := "-"
		if req.StatusCode > 0 {
			code = fmt.Sprintf("%d", req.StatusCode)
		}

		cost := "-"
		if req.Cost > 0 {
			cost = formatCost(req.Cost)
		}

		proxy := req.ProxyName
		if proxy == "" {
			proxy = "-"
		}

		fmt.Fprintf(
			w,
			"%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			req.ID,
			req.StatusText,
			code,
			req.Model,
			req.Path,
			duration,
			tokens,
			cost,
			proxy,
		)
	}
	_ = w.Flush()
}

func renderRequestDetail(out io.Writer, snapshot *SessionHistorySnapshot, req SessionHistoryRequest) {
	fmt.Fprintf(out, "Session:   %s\n", snapshot.SessionID)
	fmt.Fprintf(out, "Request:   #%d\n", req.ID)
	fmt.Fprintf(out, "Endpoint:  %s %s\n", req.Method, req.Path)
	fmt.Fprintf(out, "Model:     %s\n", req.Model)
	fmt.Fprintf(out, "Status:    %s (%d)\n", req.StatusText, req.StatusCode)
	fmt.Fprintf(out, "Start:     %s\n", req.StartTime.Format(time.RFC3339))

	if req.DurationMs > 0 {
		fmt.Fprintf(out, "Duration:  %s\n", formatDuration(time.Duration(req.DurationMs)*time.Millisecond))
	}
	if req.TTFTMs > 0 {
		fmt.Fprintf(out, "TTFT:      %s\n", formatDuration(time.Duration(req.TTFTMs)*time.Millisecond))
	}
	if req.InputTokens > 0 || req.OutputTokens > 0 {
		fmt.Fprintf(out, "Tokens:    in=%d out=%d\n", req.InputTokens, req.OutputTokens)
	}
	if req.Cost > 0 {
		fmt.Fprintf(out, "Cost:      %s\n", formatCost(req.Cost))
	}
	if req.ProxyName != "" {
		fmt.Fprintf(out, "Proxy:     %s (%s)\n", req.ProxyName, req.ProxyListen)
	}
	fmt.Fprintln(out)

	writeHeaders := func(label string, headers map[string][]string) {
		fmt.Fprintf(out, "%s:\n", label)
		if len(headers) == 0 {
			fmt.Fprintln(out, "  (none)")
			fmt.Fprintln(out)
			return
		}
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(out, "  %s: %s\n", key, strings.Join(headers[key], ", "))
		}
		fmt.Fprintln(out)
	}

	writeHeaders("Request headers", req.RequestHeaders)
	writeHeaders("Response headers", req.ResponseHeaders)

	fmt.Fprintln(out, "Request body:")
	if req.RequestBody == "" {
		fmt.Fprintln(out, "(empty)")
	} else {
		fmt.Fprintln(out, req.RequestBody)
	}
	if req.RequestBodyTruncated {
		fmt.Fprintln(out, "\n(note: request body truncated in session history)")
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Response body:")
	if req.ResponseBody == "" {
		fmt.Fprintln(out, "(empty)")
	} else {
		fmt.Fprintln(out, req.ResponseBody)
	}
	if req.ResponseBodyTruncated {
		fmt.Fprintln(out, "\n(note: response body truncated in session history)")
	}
}
