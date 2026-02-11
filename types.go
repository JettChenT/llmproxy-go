package main

import (
	"time"
)

// Request status
type RequestStatus int

const (
	StatusPending RequestStatus = iota
	StatusComplete
	StatusError
)

// LLMRequest represents a captured LLM request
type LLMRequest struct {
	ID              int
	Method          string
	Path            string
	Host            string
	URL             string
	Model           string
	Status          RequestStatus
	StatusCode      int
	StartTime       time.Time
	Duration        time.Duration
	TTFT            time.Duration // Time to first token (first response byte)
	RequestHeaders  map[string][]string
	ResponseHeaders map[string][]string
	RequestBody     []byte
	ResponseBody    []byte
	RequestSize     int
	ResponseSize    int
	IsStreaming     bool

	// Token usage and cost tracking
	EstimatedInputTokens int     // Estimated from request body length / 4
	InputTokens          int     // Actual from response usage.prompt_tokens
	OutputTokens         int     // Actual from response usage.completion_tokens
	ProviderID           string  // Detected provider (e.g., "openai", "anthropic")
	Cost                 float64 // Calculated cost in USD

	// Cache tracking
	CachedResponse bool // True if this response came from cache

	// Multi-proxy tracking
	ProxyName   string // Name of the proxy instance that handled this request
	ProxyListen string // Listen address of the proxy instance
}

// OpenAI tool call types
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Index    int              `json:"index,omitempty"`
	Function ToolCallFunction `json:"function"`
}

// OpenAI message types
type OpenAIMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"` // Can be string or array for vision
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

// Anthropic content block types
type AnthropicContentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`        // for tool_use
	Name      string      `json:"name,omitempty"`      // for tool_use
	Input     interface{} `json:"input,omitempty"`      // for tool_use
	Thinking  string      `json:"thinking,omitempty"`   // for thinking
	Signature string      `json:"signature,omitempty"`  // for thinking
}

// Anthropic message types
type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []AnthropicContentBlock
}

type AnthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []AnthropicMessage `json:"messages"`
	System      interface{}        `json:"system,omitempty"` // string or []content blocks
	MaxTokens   int                `json:"max_tokens"`
	Stream      bool               `json:"stream,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []AnthropicContentBlock `json:"content"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Usage      AnthropicUsage          `json:"usage"`
}

// Tab types for detail view
type Tab int

const (
	TabMessages Tab = iota
	TabOutput
	TabRawInput
	TabRawOutput
)

// SortField represents fields the request list can be sorted by
type SortField int

const (
	SortByNone SortField = iota
	SortByID
	SortByStatus
	SortByModel
	SortByCode
	SortBySize
	SortByDuration
	SortByInputTokens
	SortByOutputTokens
	SortByCost
)

// SortDirection represents ascending or descending sort
type SortDirection int

const (
	SortAsc SortDirection = iota
	SortDesc
)

// ImageRef represents a reference to an image found in a request/response
type ImageRef struct {
	Index   int    // 1-based index for display
	URL     string // URL or base64 data URL
	IsBase64 bool  // True if the URL is a base64 data URL
}
