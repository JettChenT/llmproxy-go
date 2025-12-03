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
	statusCode  int
	body        *bytes.Buffer
	wroteHeader bool
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
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// Header returns the header map that will be sent by WriteHeader
func (r *responseRecorder) Header() http.Header {
	return r.ResponseWriter.Header()
}

func isLLMEndpoint(path string) bool {
	llmPaths := []string{
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/embeddings",
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

		// Generate cache key and check cache (only for non-streaming requests)
		cache := GetCache()
		cacheKey := GenerateCacheKey(r.URL.Path, requestBody)
		var cachedEntry *CacheEntry
		var cacheHit bool

		if !isStreaming {
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

		// Notify TUI
		if program != nil {
			program.Send(requestAddedMsg{req: req})
		}

		// Write to tape if recording
		if tapeWriter != nil {
			tapeWriter.WriteRequestStart(req)
		}

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

			// Update request with cached response
			req.Duration = time.Since(startTime)
			req.StatusCode = cachedEntry.StatusCode
			req.ResponseHeaders = cachedEntry.ResponseHeaders
			req.ResponseBody = cachedEntry.ResponseBody
			req.ResponseSize = len(cachedEntry.ResponseBody)

			if cachedEntry.StatusCode >= 200 && cachedEntry.StatusCode < 300 {
				req.Status = StatusComplete
			} else {
				req.Status = StatusError
			}

			// Extract token usage from cached response
			go extractTokenUsage(req, cachedEntry.ResponseBody)

			// Write to tape if recording
			if tapeWriter != nil {
				tapeWriter.WriteRequestComplete(req)
			}

			// Notify TUI
			if program != nil {
				program.Send(requestUpdatedMsg{req: req})
			}
			return
		}

		// Proxy the request
		recorder := newResponseRecorder(w)
		proxy.ServeHTTP(recorder, r)

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

		// Update request with response
		req.Duration = time.Since(startTime)
		req.StatusCode = recorder.statusCode
		req.ResponseHeaders = respHeaders
		req.ResponseBody = decompressedBody
		req.ResponseSize = len(responseBody) // Original size

		if recorder.statusCode >= 200 && recorder.statusCode < 300 {
			req.Status = StatusComplete

			// Store successful response in cache (non-streaming only)
			if !isStreaming {
				cacheEntry := &CacheEntry{
					ResponseBody:    decompressedBody,
					ResponseHeaders: respHeaders,
					StatusCode:      recorder.statusCode,
					Duration:        req.Duration,
					CreatedAt:       time.Now(),
				}
				cache.Set(cacheKey, cacheEntry)
			}
		} else {
			req.Status = StatusError
		}

		// Extract token usage from response (non-blocking)
		go extractTokenUsage(req, decompressedBody)

		// Write to tape if recording
		if tapeWriter != nil {
			tapeWriter.WriteRequestComplete(req)
		}

		// Notify TUI
		if program != nil {
			program.Send(requestUpdatedMsg{req: req})
		}
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

	// Try to parse as OpenAI-style response with usage field
	var resp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return
	}

	// Update token counts
	req.InputTokens = resp.Usage.PromptTokens
	req.OutputTokens = resp.Usage.CompletionTokens

	// Calculate cost if we have provider and model info
	if req.Model != "" {
		cost := GetModelCost(req.ProviderID, req.Model)
		if cost != nil {
			req.Cost = CalculateCost(cost, req.InputTokens, req.OutputTokens)
		}
	}

	// Notify TUI of the update (if tokens were extracted)
	if program != nil && (req.InputTokens > 0 || req.OutputTokens > 0) {
		program.Send(requestUpdatedMsg{req: req})
	}
}
