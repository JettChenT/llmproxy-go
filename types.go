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
