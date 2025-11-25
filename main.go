package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/fatih/color"
)

var (
	cyan    = color.New(color.FgCyan).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	yellow  = color.New(color.FgYellow).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	magenta = color.New(color.FgMagenta).SprintFunc()
	dim     = color.New(color.Faint).SprintFunc()
	bold    = color.New(color.Bold).SprintFunc()
)

// responseRecorder wraps http.ResponseWriter to capture the response
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	bodyLen    int
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.bodyLen += len(b)
	return r.ResponseWriter.Write(b)
}

// openAIRequest represents the structure we care about from OpenAI requests
type openAIRequest struct {
	Model string `json:"model"`
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

func statusColor(code int) string {
	switch {
	case code >= 200 && code < 300:
		return green(fmt.Sprintf("%d", code))
	case code >= 300 && code < 400:
		return yellow(fmt.Sprintf("%d", code))
	default:
		return red(fmt.Sprintf("%d", code))
	}
}

func main() {
	listenAddr := flag.String("listen", ":8080", "Address to listen on")
	targetURL := flag.String("target", "http://localhost:3000", "Target URL to proxy to")
	flag.Parse()

	target, err := url.Parse(*targetURL)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
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
		model := "-"

		if isLLM && len(requestBody) > 0 {
			var req openAIRequest
			if json.Unmarshal(requestBody, &req) == nil && req.Model != "" {
				model = req.Model
			}
			fmt.Printf("%s %s %s %s %s\n",
				cyan("→"),
				bold(r.Method),
				dim(r.URL.Path),
				magenta(model),
				dim(fmt.Sprintf("%d bytes", len(requestBody))),
			)
		}

		recorder := newResponseRecorder(w)
		proxy.ServeHTTP(recorder, r)
		duration := time.Since(startTime)

		if isLLM {
			fmt.Printf("%s %s %s %s %s\n",
				green("←"),
				statusColor(recorder.statusCode),
				magenta(model),
				dim(fmt.Sprintf("%d bytes", recorder.bodyLen)),
				yellow(duration.Round(time.Millisecond).String()),
			)
		}
	})

	fmt.Printf("%s %s %s %s\n",
		bold("Proxy"),
		cyan(*listenAddr),
		dim("→"),
		green(*targetURL),
	)

	if err := http.ListenAndServe(*listenAddr, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
