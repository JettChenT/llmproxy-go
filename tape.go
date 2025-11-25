package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// TapeEventType represents the type of event in a tape
type TapeEventType string

const (
	EventSessionStart   TapeEventType = "session_start"
	EventRequestStart   TapeEventType = "request_start"
	EventRequestUpdate  TapeEventType = "request_update"
	EventRequestComplete TapeEventType = "request_complete"
	EventSessionEnd     TapeEventType = "session_end"
)

// TapeEvent represents a single event in a tape file
type TapeEvent struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      TapeEventType   `json:"type"`
	Sequence  int             `json:"seq"`
	Data      json.RawMessage `json:"data"`
}

// TapeSessionData contains session metadata
type TapeSessionData struct {
	ListenAddr string    `json:"listen_addr"`
	TargetURL  string    `json:"target_url"`
	StartTime  time.Time `json:"start_time"`
	Version    string    `json:"version"`
}

// TapeRequestData contains request event data
type TapeRequestData struct {
	ID                   int                 `json:"id"`
	Method               string              `json:"method"`
	Path                 string              `json:"path"`
	Host                 string              `json:"host"`
	URL                  string              `json:"url"`
	Model                string              `json:"model"`
	Status               RequestStatus       `json:"status"`
	StatusCode           int                 `json:"status_code"`
	StartTime            time.Time           `json:"start_time"`
	Duration             time.Duration       `json:"duration"`
	RequestHeaders       map[string][]string `json:"request_headers,omitempty"`
	ResponseHeaders      map[string][]string `json:"response_headers,omitempty"`
	RequestBody          []byte              `json:"request_body,omitempty"`
	ResponseBody         []byte              `json:"response_body,omitempty"`
	RequestSize          int                 `json:"request_size"`
	ResponseSize         int                 `json:"response_size"`
	IsStreaming          bool                `json:"is_streaming"`
	EstimatedInputTokens int                 `json:"estimated_input_tokens,omitempty"`
	InputTokens          int                 `json:"input_tokens,omitempty"`
	OutputTokens         int                 `json:"output_tokens,omitempty"`
	ProviderID           string              `json:"provider_id,omitempty"`
	Cost                 float64             `json:"cost,omitempty"`
}

// Tape represents a loaded tape with all events
type Tape struct {
	FilePath    string
	Session     TapeSessionData
	Events      []TapeEvent
	Requests    []*LLMRequest
	RequestMap  map[int]*LLMRequest // Map request ID to request
	Timeline    []TimelineEntry     // Events sorted by time for replay
	CurrentTime time.Time           // Current position in replay
	StartTime   time.Time           // Tape start time
	EndTime     time.Time           // Tape end time
	Duration    time.Duration       // Total tape duration
}

// TimelineEntry represents a point in the timeline
type TimelineEntry struct {
	Time    time.Time
	Event   *TapeEvent
	Request *LLMRequest
}

// TapeWriter handles writing events to a tape file
type TapeWriter struct {
	file     *os.File
	encoder  *json.Encoder
	sequence int
}

// NewTapeWriter creates a new tape writer
func NewTapeWriter(filename string) (*TapeWriter, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create tape file: %w", err)
	}

	return &TapeWriter{
		file:     file,
		encoder:  json.NewEncoder(file),
		sequence: 0,
	}, nil
}

// WriteEvent writes a single event to the tape
func (tw *TapeWriter) WriteEvent(eventType TapeEventType, data interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	tw.sequence++
	event := TapeEvent{
		Timestamp: time.Now(),
		Type:      eventType,
		Sequence:  tw.sequence,
		Data:      dataJSON,
	}

	if err := tw.encoder.Encode(event); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	// Flush to ensure event is written immediately
	return tw.file.Sync()
}

// WriteSessionStart writes the session start event
func (tw *TapeWriter) WriteSessionStart(listenAddr, targetURL string) error {
	return tw.WriteEvent(EventSessionStart, TapeSessionData{
		ListenAddr: listenAddr,
		TargetURL:  targetURL,
		StartTime:  time.Now(),
		Version:    "1.0",
	})
}

// WriteRequestStart writes a request start event
func (tw *TapeWriter) WriteRequestStart(req *LLMRequest) error {
	return tw.WriteEvent(EventRequestStart, requestToTapeData(req))
}

// WriteRequestUpdate writes a request update event (for streaming updates)
func (tw *TapeWriter) WriteRequestUpdate(req *LLMRequest) error {
	return tw.WriteEvent(EventRequestUpdate, requestToTapeData(req))
}

// WriteRequestComplete writes a request complete event
func (tw *TapeWriter) WriteRequestComplete(req *LLMRequest) error {
	return tw.WriteEvent(EventRequestComplete, requestToTapeData(req))
}

// WriteSessionEnd writes the session end event
func (tw *TapeWriter) WriteSessionEnd() error {
	return tw.WriteEvent(EventSessionEnd, map[string]interface{}{
		"end_time": time.Now(),
	})
}

// Close closes the tape writer
func (tw *TapeWriter) Close() error {
	tw.WriteSessionEnd()
	return tw.file.Close()
}

// requestToTapeData converts an LLMRequest to TapeRequestData
func requestToTapeData(req *LLMRequest) TapeRequestData {
	return TapeRequestData{
		ID:                   req.ID,
		Method:               req.Method,
		Path:                 req.Path,
		Host:                 req.Host,
		URL:                  req.URL,
		Model:                req.Model,
		Status:               req.Status,
		StatusCode:           req.StatusCode,
		StartTime:            req.StartTime,
		Duration:             req.Duration,
		RequestHeaders:       req.RequestHeaders,
		ResponseHeaders:      req.ResponseHeaders,
		RequestBody:          req.RequestBody,
		ResponseBody:         req.ResponseBody,
		RequestSize:          req.RequestSize,
		ResponseSize:         req.ResponseSize,
		IsStreaming:          req.IsStreaming,
		EstimatedInputTokens: req.EstimatedInputTokens,
		InputTokens:          req.InputTokens,
		OutputTokens:         req.OutputTokens,
		ProviderID:           req.ProviderID,
		Cost:                 req.Cost,
	}
}

// tapeDataToRequest converts TapeRequestData to LLMRequest
func tapeDataToRequest(data TapeRequestData) *LLMRequest {
	return &LLMRequest{
		ID:                   data.ID,
		Method:               data.Method,
		Path:                 data.Path,
		Host:                 data.Host,
		URL:                  data.URL,
		Model:                data.Model,
		Status:               data.Status,
		StatusCode:           data.StatusCode,
		StartTime:            data.StartTime,
		Duration:             data.Duration,
		RequestHeaders:       data.RequestHeaders,
		ResponseHeaders:      data.ResponseHeaders,
		RequestBody:          data.RequestBody,
		ResponseBody:         data.ResponseBody,
		RequestSize:          data.RequestSize,
		ResponseSize:         data.ResponseSize,
		IsStreaming:          data.IsStreaming,
		EstimatedInputTokens: data.EstimatedInputTokens,
		InputTokens:          data.InputTokens,
		OutputTokens:         data.OutputTokens,
		ProviderID:           data.ProviderID,
		Cost:                 data.Cost,
	}
}

// LoadTape loads a tape file and returns all events
func LoadTape(filename string) (*Tape, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open tape file: %w", err)
	}
	defer file.Close()

	tape := &Tape{
		FilePath:   filename,
		Events:     make([]TapeEvent, 0),
		Requests:   make([]*LLMRequest, 0),
		RequestMap: make(map[int]*LLMRequest),
		Timeline:   make([]TimelineEntry, 0),
	}

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large events
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max

	for scanner.Scan() {
		var event TapeEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // Skip malformed lines
		}

		tape.Events = append(tape.Events, event)

		// Process event based on type
		switch event.Type {
		case EventSessionStart:
			var sessionData TapeSessionData
			if err := json.Unmarshal(event.Data, &sessionData); err == nil {
				tape.Session = sessionData
				tape.StartTime = sessionData.StartTime
			}

		case EventRequestStart, EventRequestUpdate, EventRequestComplete:
			var reqData TapeRequestData
			if err := json.Unmarshal(event.Data, &reqData); err == nil {
				// Update or create request
				if existing, ok := tape.RequestMap[reqData.ID]; ok {
					// Update existing request
					*existing = *tapeDataToRequest(reqData)
				} else {
					// Create new request
					req := tapeDataToRequest(reqData)
					tape.Requests = append(tape.Requests, req)
					tape.RequestMap[reqData.ID] = req
				}

				// Add to timeline
				tape.Timeline = append(tape.Timeline, TimelineEntry{
					Time:    event.Timestamp,
					Event:   &tape.Events[len(tape.Events)-1],
					Request: tape.RequestMap[reqData.ID],
				})
			}

		case EventSessionEnd:
			tape.EndTime = event.Timestamp
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading tape file: %w", err)
	}

	// Calculate duration
	if !tape.EndTime.IsZero() && !tape.StartTime.IsZero() {
		tape.Duration = tape.EndTime.Sub(tape.StartTime)
	} else if len(tape.Timeline) > 0 {
		tape.EndTime = tape.Timeline[len(tape.Timeline)-1].Time
		tape.Duration = tape.EndTime.Sub(tape.StartTime)
	}

	// Sort requests by ID
	sort.Slice(tape.Requests, func(i, j int) bool {
		return tape.Requests[i].ID < tape.Requests[j].ID
	})

	// Sort timeline by time
	sort.Slice(tape.Timeline, func(i, j int) bool {
		return tape.Timeline[i].Time.Before(tape.Timeline[j].Time)
	})

	tape.CurrentTime = tape.StartTime

	return tape, nil
}

// SaveSessionToTape saves the current session to a tape file
func SaveSessionToTape(filename string, listenAddr, targetURL string) error {
	writer, err := NewTapeWriter(filename)
	if err != nil {
		return err
	}
	defer writer.Close()

	// Write session start
	requestsMu.RLock()
	defer requestsMu.RUnlock()

	// Find earliest request time for session start
	sessionStart := time.Now()
	if len(requests) > 0 {
		sessionStart = requests[0].StartTime
	}

	if err := writer.WriteEvent(EventSessionStart, TapeSessionData{
		ListenAddr: listenAddr,
		TargetURL:  targetURL,
		StartTime:  sessionStart,
		Version:    "1.0",
	}); err != nil {
		return err
	}

	// Write all requests
	for _, req := range requests {
		// Create a pending version of the request for the start event
		pendingReq := requestToTapeData(req)
		pendingReq.Status = StatusPending
		pendingReq.StatusCode = 0
		pendingReq.Duration = 0
		pendingReq.ResponseHeaders = nil
		pendingReq.ResponseBody = nil
		pendingReq.ResponseSize = 0
		pendingReq.InputTokens = 0
		pendingReq.OutputTokens = 0
		pendingReq.Cost = 0

		// Write request start event at request start time
		startEvent := TapeEvent{
			Timestamp: req.StartTime,
			Type:      EventRequestStart,
			Sequence:  writer.sequence + 1,
		}
		startData, _ := json.Marshal(pendingReq)
		startEvent.Data = startData
		writer.sequence++
		writer.encoder.Encode(startEvent)

		// If complete, write complete event with full data
		if req.Status != StatusPending {
			completeEvent := TapeEvent{
				Timestamp: req.StartTime.Add(req.Duration),
				Type:      EventRequestComplete,
				Sequence:  writer.sequence + 1,
			}
			completeData, _ := json.Marshal(requestToTapeData(req))
			completeEvent.Data = completeData
			writer.sequence++
			writer.encoder.Encode(completeEvent)
		}
	}

	return nil
}

// GetRequestsAtTime returns all requests that exist at a given time
func (t *Tape) GetRequestsAtTime(targetTime time.Time) []*LLMRequest {
	result := make([]*LLMRequest, 0)
	requestStates := make(map[int]*LLMRequest)

	for _, entry := range t.Timeline {
		if entry.Time.After(targetTime) {
			break
		}

		// Apply event to build state at this time
		if entry.Event != nil {
			var reqData TapeRequestData
			if err := json.Unmarshal(entry.Event.Data, &reqData); err == nil {
				req := tapeDataToRequest(reqData)
				requestStates[req.ID] = req
			}
		}
	}

	// Convert map to slice and sort
	for _, req := range requestStates {
		result = append(result, req)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result
}

// SeekToTime moves the tape position to a specific time
func (t *Tape) SeekToTime(targetTime time.Time) {
	if targetTime.Before(t.StartTime) {
		t.CurrentTime = t.StartTime
	} else if targetTime.After(t.EndTime) {
		t.CurrentTime = t.EndTime
	} else {
		t.CurrentTime = targetTime
	}
}

// SeekToPercent seeks to a percentage through the tape (0.0 - 1.0)
func (t *Tape) SeekToPercent(percent float64) {
	if percent < 0 {
		percent = 0
	} else if percent > 1 {
		percent = 1
	}

	offset := time.Duration(float64(t.Duration) * percent)
	t.CurrentTime = t.StartTime.Add(offset)
}

// GetProgress returns the current progress through the tape (0.0 - 1.0)
func (t *Tape) GetProgress() float64 {
	if t.Duration == 0 {
		return 0
	}
	elapsed := t.CurrentTime.Sub(t.StartTime)
	return float64(elapsed) / float64(t.Duration)
}

// StepForward moves to the next event
func (t *Tape) StepForward() bool {
	for i, entry := range t.Timeline {
		if entry.Time.After(t.CurrentTime) {
			t.CurrentTime = entry.Time
			return i < len(t.Timeline)-1
		}
	}
	return false
}

// StepBackward moves to the previous event
func (t *Tape) StepBackward() bool {
	var lastBefore *TimelineEntry
	for i := range t.Timeline {
		if t.Timeline[i].Time.Before(t.CurrentTime) {
			lastBefore = &t.Timeline[i]
		} else {
			break
		}
	}
	if lastBefore != nil {
		t.CurrentTime = lastBefore.Time
		return true
	}
	return false
}

