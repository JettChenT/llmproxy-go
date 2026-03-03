package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Global request store
var (
	requests   []*LLMRequest
	requestsMu sync.RWMutex
	requestID  int
	program    *tea.Program
	tapeWriter *TapeWriter // Global tape writer for recording
)

// Response recorder for capturing response
type responseRecorder struct {
	http.ResponseWriter
	statusCode     int
	body           *bytes.Buffer
	wroteHeader    bool
	firstWriteTime time.Time // Time of first Write() call (TTFT proxy)
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.firstWriteTime.IsZero() {
		r.firstWriteTime = time.Now()
	}
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// Header returns the header map that will be sent by WriteHeader
func (r *responseRecorder) Header() http.Header {
	return r.ResponseWriter.Header()
}

// Flush implements http.Flusher so that SSE streaming works through the proxy.
func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func isLLMEndpoint(path string) bool {
	llmPaths := []string{
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/embeddings",
		"/v1/messages",
		"/chat/completions",
		"/completions",
	}
	for _, p := range llmPaths {
		if strings.HasSuffix(path, p) {
			return true
		}
	}
	return false
}

// isAnthropicEndpoint returns true if the path is an Anthropic Messages API endpoint
func isAnthropicEndpoint(path string) bool {
	return strings.HasSuffix(path, "/v1/messages")
}

// shouldSkipCache checks if the request has headers indicating caching should be skipped.
// Supports Cache-Control: no-cache/no-store and X-No-Cache header.
func shouldSkipCache(r *http.Request) bool {
	// Check X-No-Cache header (any value means skip cache)
	if r.Header.Get("X-No-Cache") != "" {
		return true
	}

	// Check Cache-Control header for no-cache or no-store directives
	cacheControl := strings.ToLower(r.Header.Get("Cache-Control"))
	if strings.Contains(cacheControl, "no-cache") || strings.Contains(cacheControl, "no-store") {
		return true
	}

	return false
}

// decompressIfNeeded decompresses gzip content if needed
func decompressIfNeeded(data []byte, contentEncoding string) []byte {
	if !strings.Contains(strings.ToLower(contentEncoding), "gzip") {
		return data
	}

	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		// Not actually gzip or corrupted, return as-is
		return data
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		// Decompression failed, return original
		return data
	}

	return decompressed
}

// ProxyInstance represents a running proxy instance
type ProxyInstance struct {
	Name       string
	ListenAddr string
	TargetURL  string
	server     *http.Server
}

// startProxy starts a single proxy instance (legacy function for backwards compatibility)
func startProxy(listenAddr, targetURL string) {
	StartProxyInstance("default", listenAddr, targetURL)
}

// StartProxyInstance starts a named proxy instance
func StartProxyInstance(name, listenAddr, targetURL string) error {
	// Load models.dev database in background (only once)
	LoadModelsDB()

	target, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid target URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}

	// Handle proxy errors (upstream unreachable, DNS failure, etc.)
	// Write a 502 status so the request is properly marked as failed in the TUI.
	// Don't log to stderr as it disrupts the TUI layout.
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.WriteHeader(http.StatusBadGateway)
	}

	// Flush immediately for streaming responses (SSE)
	proxy.FlushInterval = -1

	// Modify response to capture and potentially decompress
	proxy.ModifyResponse = func(resp *http.Response) error {
		return nil
	}

	// Create a new ServeMux for this proxy instance
	mux := http.NewServeMux()
	mux.HandleFunc("/", createProxyHandler(name, listenAddr, target, proxy))

	server := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[%s] Server error on %s: %v", name, listenAddr, err)
		}
	}()

	return nil
}

// createProxyHandler creates an HTTP handler for a proxy instance
func createProxyHandler(proxyName, listenAddr string, target *url.URL, proxy *httputil.ReverseProxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Read request body
		var requestBody []byte
		if r.Body != nil {
			requestBody, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		isLLM := isLLMEndpoint(r.URL.Path)
		if !isLLM {
			proxy.ServeHTTP(w, r)
			return
		}

		// Parse model from request
		model := "unknown"
		var openAIReq OpenAIRequest
		if json.Unmarshal(requestBody, &openAIReq) == nil && openAIReq.Model != "" {
			model = openAIReq.Model
		}

		// Check if streaming is requested
		isStreaming := openAIReq.Stream

		// Copy request headers
		reqHeaders := make(map[string][]string)
		for k, v := range r.Header {
			reqHeaders[k] = v
		}

		// Build full URL
		fullURL := r.URL.String()
		if r.URL.Host == "" {
			fullURL = target.Scheme + "://" + target.Host + r.URL.Path
			if r.URL.RawQuery != "" {
				fullURL += "?" + r.URL.RawQuery
			}
		}

		// Detect provider from target URL
		providerID, _ := FindProviderByURL(fullURL)

		// Estimate input tokens from request body size
		estimatedTokens := EstimateInputTokens(string(requestBody))

		// Generate cache key and check cache (only for non-streaming requests without no-cache header)
		cache := GetCache()
		cacheKey := GenerateCacheKey(r.URL.Path, requestBody)
		var cachedEntry *CacheEntry
		var cacheHit bool
		skipCache := shouldSkipCache(r)

		if !isStreaming && !skipCache {
			cachedEntry, cacheHit = cache.Get(cacheKey)
		}

		// Create request entry
		requestsMu.Lock()
		requestID++
		req := &LLMRequest{
			ID:                   requestID,
			Method:               r.Method,
			Path:                 r.URL.Path,
			Host:                 target.Host,
			URL:                  fullURL,
			Model:                model,
			Status:               StatusPending,
			StartTime:            startTime,
			RequestHeaders:       reqHeaders,
			RequestBody:          requestBody,
			RequestSize:          len(requestBody),
			IsStreaming:          isStreaming,
			ProviderID:           providerID,
			EstimatedInputTokens: estimatedTokens,
			CachedResponse:       cacheHit,
			ProxyName:            proxyName,
			ProxyListen:          listenAddr,
		}
		requests = append(requests, req)
		requestsMu.Unlock()
		RecordSessionRequest(req)

		// Notify TUI
		if program != nil {
			program.Send(requestAddedMsg{req: req})
		}

		// Write to tape if recording
		if tapeWriter != nil {
			tapeWriter.WriteRequestStart(req)
		}

		var finalizeOnce sync.Once
		finalize := func(statusCode int, respHeaders map[string][]string, responseBody []byte, responseSize int) {
			finalizeOnce.Do(func() {
				req.Duration = time.Since(startTime)
				req.StatusCode = statusCode
				req.ResponseHeaders = respHeaders
				req.ResponseBody = responseBody
				req.ResponseSize = responseSize

				if statusCode >= 200 && statusCode < 300 {
					req.Status = StatusComplete

					// Store successful response in cache (non-streaming only, respects no-cache header)
					if !isStreaming && !skipCache {
						cacheEntry := &CacheEntry{
							ResponseBody:    responseBody,
							ResponseHeaders: respHeaders,
							StatusCode:      statusCode,
							Duration:        req.Duration,
							CreatedAt:       time.Now(),
						}
						cache.Set(cacheKey, cacheEntry)
					}
				} else {
					req.Status = StatusError
				}

				if len(responseBody) > 0 {
					// Extract token usage from response (non-blocking)
					go extractTokenUsage(req, responseBody)
				}

				RecordSessionRequest(req)

				// Write to tape if recording
				if tapeWriter != nil {
					tapeWriter.WriteRequestComplete(req)
				}

				// Notify TUI
				if program != nil {
					program.Send(requestUpdatedMsg{req: req})
				}
			})
		}

		cancelDone := make(chan struct{})
		go func() {
			select {
			case <-r.Context().Done():
				finalize(499, nil, nil, 0)
			case <-cancelDone:
			}
		}()

		// Handle cache hit
		if cacheHit && cachedEntry != nil {
			// Simulate latency if configured
			config := GetCacheConfig()
			if config.SimulateLatency && cachedEntry.Duration > 0 {
				time.Sleep(cachedEntry.Duration)
			}

			// Write cached response headers, but skip headers that don't apply
			// to the decompressed cached body
			skipHeaders := map[string]bool{
				"Content-Encoding":  true, // Body is stored decompressed
				"Content-Length":    true, // Will set correct length below
				"Transfer-Encoding": true, // Not chunked anymore
				"Connection":        true, // Let Go handle this
				"Keep-Alive":        true, // Let Go handle this
			}
			for k, v := range cachedEntry.ResponseHeaders {
				if skipHeaders[k] {
					continue
				}
				for _, val := range v {
					w.Header().Add(k, val)
				}
			}
			// Set correct Content-Length for the decompressed body
			w.Header().Set("Content-Length", strconv.Itoa(len(cachedEntry.ResponseBody)))
			w.WriteHeader(cachedEntry.StatusCode)
			w.Write(cachedEntry.ResponseBody)
			close(cancelDone)
			finalize(cachedEntry.StatusCode, cachedEntry.ResponseHeaders, cachedEntry.ResponseBody, len(cachedEntry.ResponseBody))
			return
		}

		// Proxy the request
		recorder := newResponseRecorder(w)
		proxy.ServeHTTP(recorder, r)
		close(cancelDone)
		if r.Context().Err() != nil {
			return
		}

		// Track TTFT (time from request start to first response byte)
		if !recorder.firstWriteTime.IsZero() {
			req.TTFT = recorder.firstWriteTime.Sub(startTime)
		}

		// Get Content-Encoding from response headers
		contentEncoding := recorder.Header().Get("Content-Encoding")

		// Copy response headers
		respHeaders := make(map[string][]string)
		for k, v := range recorder.Header() {
			respHeaders[k] = v
		}

		// Decompress if needed for storage (we still pass compressed data to client)
		responseBody := recorder.body.Bytes()
		decompressedBody := decompressIfNeeded(responseBody, contentEncoding)

		finalize(recorder.statusCode, respHeaders, decompressedBody, len(responseBody))
	}
}

// StartMultipleProxies starts multiple proxy instances from configuration
func StartMultipleProxies(proxies []ProxyConfig) error {
	for _, p := range proxies {
		name := p.Name
		if name == "" {
			name = p.Listen
		}
		if err := StartProxyInstance(name, p.Listen, p.Target); err != nil {
			return fmt.Errorf("failed to start proxy %s: %w", name, err)
		}
		log.Printf("Started proxy [%s]: %s -> %s", name, p.Listen, p.Target)
	}
	return nil
}

// extractTokenUsage extracts token usage from response and calculates cost
// This runs in a goroutine to not block the main proxy flow
func extractTokenUsage(req *LLMRequest, responseBody []byte) {
	// Recover from any panics to ensure this never crashes the proxy
	defer func() {
		if r := recover(); r != nil {
			// Silently ignore panics in cost calculation
		}
	}()

	if len(responseBody) == 0 {
		return
	}

	// Check for SSE data (streaming response)
	// SSE streams may start with comment lines (": comment"), event lines, or data lines
	trimmed := strings.TrimSpace(string(responseBody))
	if strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, ":") {
		providerCost := extractTokenUsageFromSSE(req, responseBody)
		if providerCost > 0 {
			// Prefer provider-reported cost (e.g., OpenRouter)
			req.Cost = providerCost
		} else if req.Model != "" && (req.InputTokens > 0 || req.OutputTokens > 0) {
			cost := GetModelCost(req.ProviderID, req.Model)
			if cost != nil {
				req.Cost = CalculateCost(cost, req.InputTokens, req.OutputTokens)
			}
		}
		RecordSessionRequest(req)
		if program != nil && (req.InputTokens > 0 || req.OutputTokens > 0) {
			program.Send(requestUpdatedMsg{req: req})
		}
		return
	}

	// Try to parse response with usage field (supports both OpenAI and Anthropic formats)
	var resp struct {
		Usage struct {
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
			TotalTokens      int     `json:"total_tokens"`
			InputTokens      int     `json:"input_tokens"`
			OutputTokens     int     `json:"output_tokens"`
			Cost             float64 `json:"cost"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return
	}

	// Update token counts (OpenAI format)
	req.InputTokens = resp.Usage.PromptTokens
	req.OutputTokens = resp.Usage.CompletionTokens

	// Anthropic format fallback (input_tokens, output_tokens)
	if req.InputTokens == 0 && resp.Usage.InputTokens > 0 {
		req.InputTokens = resp.Usage.InputTokens
	}
	if req.OutputTokens == 0 && resp.Usage.OutputTokens > 0 {
		req.OutputTokens = resp.Usage.OutputTokens
	}

	// Calculate cost: prefer provider-reported cost, fallback to model DB lookup
	if resp.Usage.Cost > 0 {
		req.Cost = resp.Usage.Cost
	} else if req.Model != "" {
		cost := GetModelCost(req.ProviderID, req.Model)
		if cost != nil {
			req.Cost = CalculateCost(cost, req.InputTokens, req.OutputTokens)
		}
	}
	RecordSessionRequest(req)

	// Notify TUI of the update (if tokens were extracted)
	if program != nil && (req.InputTokens > 0 || req.OutputTokens > 0) {
		program.Send(requestUpdatedMsg{req: req})
	}
}

// reassembleSSEResponse reconstructs an OpenAIResponse from SSE streaming chunks.
// It accumulates delta.content and delta.reasoning into a single response with usage info.
func reassembleSSEResponse(data []byte) *OpenAIResponse {
	lines := strings.Split(string(data), "\n")

	resp := &OpenAIResponse{}
	// Track content and reasoning per choice index
	choiceContent := make(map[int]*strings.Builder)
	choiceReasoning := make(map[int]*strings.Builder)
	choiceFinishReason := make(map[int]string)
	choiceToolCalls := make(map[int]map[int]*ToolCall) // choice index -> tool call index -> tool call

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data:") {
			continue
		}

		jsonData := strings.TrimPrefix(line, "data: ")
		jsonData = strings.TrimPrefix(jsonData, "data:")
		jsonData = strings.TrimSpace(jsonData)
		if jsonData == "" || jsonData == "[DONE]" {
			continue
		}

		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content          string     `json:"content"`
					Role             string     `json:"role"`
					Reasoning        string     `json:"reasoning"`
					ReasoningContent string     `json:"reasoning_content"`
					ToolCalls        []ToolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
			continue
		}

		if resp.ID == "" && chunk.ID != "" {
			resp.ID = chunk.ID
		}
		if resp.Model == "" && chunk.Model != "" {
			resp.Model = chunk.Model
		}

		for _, choice := range chunk.Choices {
			idx := choice.Index
			if _, ok := choiceContent[idx]; !ok {
				choiceContent[idx] = &strings.Builder{}
				choiceReasoning[idx] = &strings.Builder{}
			}
			if choice.Delta.Content != "" {
				choiceContent[idx].WriteString(choice.Delta.Content)
			}
			if choice.Delta.Reasoning != "" {
				choiceReasoning[idx].WriteString(choice.Delta.Reasoning)
			}
			if choice.Delta.ReasoningContent != "" {
				choiceReasoning[idx].WriteString(choice.Delta.ReasoningContent)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				choiceFinishReason[idx] = *choice.FinishReason
			}
			// Accumulate tool calls
			for _, tc := range choice.Delta.ToolCalls {
				if choiceToolCalls[idx] == nil {
					choiceToolCalls[idx] = make(map[int]*ToolCall)
				}
				existing, ok := choiceToolCalls[idx][tc.Index]
				if !ok {
					newTC := ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: ToolCallFunction{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					}
					choiceToolCalls[idx][tc.Index] = &newTC
				} else {
					if tc.ID != "" {
						existing.ID = tc.ID
					}
					if tc.Function.Name != "" {
						existing.Function.Name += tc.Function.Name
					}
					existing.Function.Arguments += tc.Function.Arguments
				}
			}
		}

		if chunk.Usage.PromptTokens > 0 {
			resp.Usage.PromptTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			resp.Usage.CompletionTokens = chunk.Usage.CompletionTokens
		}
		if chunk.Usage.TotalTokens > 0 {
			resp.Usage.TotalTokens = chunk.Usage.TotalTokens
		}
	}

	// Build choices from accumulated data
	maxIdx := -1
	for idx := range choiceContent {
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	if maxIdx < 0 {
		return nil
	}

	resp.Choices = make([]OpenAIChoice, maxIdx+1)
	for i := 0; i <= maxIdx; i++ {
		choice := OpenAIChoice{Index: i}
		if cb, ok := choiceContent[i]; ok {
			choice.Message.Content = cb.String()
		}
		if rb, ok := choiceReasoning[i]; ok && rb.Len() > 0 {
			choice.Message.ReasoningContent = rb.String()
		}
		if fr, ok := choiceFinishReason[i]; ok {
			choice.FinishReason = fr
		}
		// Collect tool calls in order
		if tcs, ok := choiceToolCalls[i]; ok && len(tcs) > 0 {
			maxTCIdx := -1
			for tcIdx := range tcs {
				if tcIdx > maxTCIdx {
					maxTCIdx = tcIdx
				}
			}
			for tcIdx := 0; tcIdx <= maxTCIdx; tcIdx++ {
				if tc, ok := tcs[tcIdx]; ok {
					if tc.Type == "" {
						tc.Type = "function"
					}
					choice.Message.ToolCalls = append(choice.Message.ToolCalls, *tc)
				}
			}
		}
		choice.Message.Role = "assistant"
		resp.Choices[i] = choice
	}

	return resp
}

// isSSEData returns true if the data appears to be SSE format
func isSSEData(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))
	return strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, ":")
}

// extractTokenUsageFromSSE parses SSE events to extract token usage from streaming responses.
// Handles both Anthropic SSE (message_start/message_delta events) and OpenAI SSE (usage in data chunks).
// Returns the provider-reported cost if present in the usage data (e.g., OpenRouter).
func extractTokenUsageFromSSE(req *LLMRequest, data []byte) (providerCost float64) {
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data:") {
			continue
		}

		jsonData := strings.TrimPrefix(line, "data: ")
		jsonData = strings.TrimPrefix(jsonData, "data:")
		jsonData = strings.TrimSpace(jsonData)
		if jsonData == "" || jsonData == "[DONE]" {
			continue
		}

		var event struct {
			Type    string `json:"type"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				PromptTokens     int     `json:"prompt_tokens"`
				CompletionTokens int     `json:"completion_tokens"`
				InputTokens      int     `json:"input_tokens"`
				OutputTokens     int     `json:"output_tokens"`
				Cost             float64 `json:"cost"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}

		// Anthropic message_start: input_tokens in message.usage
		if event.Type == "message_start" && event.Message.Usage.InputTokens > 0 {
			req.InputTokens = event.Message.Usage.InputTokens
		}

		// Anthropic message_delta: output_tokens in usage
		if event.Type == "message_delta" && event.Usage.OutputTokens > 0 {
			req.OutputTokens = event.Usage.OutputTokens
		}

		// OpenAI: usage in data chunks (last chunk with stream_options include_usage)
		if event.Usage.PromptTokens > 0 {
			req.InputTokens = event.Usage.PromptTokens
		}
		if event.Usage.CompletionTokens > 0 {
			req.OutputTokens = event.Usage.CompletionTokens
		}

		// Provider-reported cost (e.g., OpenRouter includes cost in usage)
		if event.Usage.Cost > 0 {
			providerCost = event.Usage.Cost
		}
	}
	return providerCost
}
