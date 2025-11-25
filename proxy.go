package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
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

func startProxy(listenAddr, targetURL string) {
	target, err := url.Parse(targetURL)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

		// Create request entry
		requestsMu.Lock()
		requestID++
		req := &LLMRequest{
			ID:             requestID,
			Method:         r.Method,
			Path:           r.URL.Path,
			Host:           target.Host,
			URL:            fullURL,
			Model:          model,
			Status:         StatusPending,
			StartTime:      startTime,
			RequestHeaders: reqHeaders,
			RequestBody:    requestBody,
			RequestSize:    len(requestBody),
			IsStreaming:    isStreaming,
		}
		requests = append([]*LLMRequest{req}, requests...)
		requestsMu.Unlock()

		// Notify TUI
		if program != nil {
			program.Send(requestAddedMsg{req: req})
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
		} else {
			req.Status = StatusError
		}

		// Notify TUI
		if program != nil {
			program.Send(requestUpdatedMsg{req: req})
		}
	})

	go func() {
		if err := http.ListenAndServe(listenAddr, nil); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()
}
