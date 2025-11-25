package main

import (
	"encoding/json"
	"sort"
	"strings"
)

// extractSearchableText extracts all searchable text from a request (messages + response)
func (m *model) extractSearchableText(req *LLMRequest) string {
	// Check cache first
	if cached, ok := m.searchIndexCache[req.ID]; ok {
		return cached
	}

	var sb strings.Builder

	// Add model name
	sb.WriteString(req.Model)
	sb.WriteString(" ")

	// Extract request messages
	if len(req.RequestBody) > 0 {
		var openAIReq OpenAIRequest
		if err := json.Unmarshal(req.RequestBody, &openAIReq); err == nil {
			for _, msg := range openAIReq.Messages {
				sb.WriteString(msg.Role)
				sb.WriteString(" ")
				// Handle content which can be string or array
				switch c := msg.Content.(type) {
				case string:
					sb.WriteString(c)
					sb.WriteString(" ")
				case []interface{}:
					for _, part := range c {
						if p, ok := part.(map[string]interface{}); ok {
							if text, ok := p["text"].(string); ok {
								sb.WriteString(text)
								sb.WriteString(" ")
							}
						}
					}
				}
				// Add tool call info
				for _, tc := range msg.ToolCalls {
					sb.WriteString(tc.Function.Name)
					sb.WriteString(" ")
					sb.WriteString(tc.Function.Arguments)
					sb.WriteString(" ")
				}
			}
		}
	}

	// Extract response messages
	if len(req.ResponseBody) > 0 {
		var openAIResp OpenAIResponse
		if err := json.Unmarshal(req.ResponseBody, &openAIResp); err == nil {
			for _, choice := range openAIResp.Choices {
				switch c := choice.Message.Content.(type) {
				case string:
					sb.WriteString(c)
					sb.WriteString(" ")
				}
				for _, tc := range choice.Message.ToolCalls {
					sb.WriteString(tc.Function.Name)
					sb.WriteString(" ")
					sb.WriteString(tc.Function.Arguments)
					sb.WriteString(" ")
				}
			}
		}
	}

	text := strings.ToLower(sb.String())
	m.searchIndexCache[req.ID] = text
	return text
}

// filterRequests filters requests based on search query (case-insensitive substring match)
func (m *model) filterRequests() {
	if m.searchQuery == "" {
		m.filteredRequests = nil
		return
	}

	query := strings.ToLower(m.searchQuery)
	m.filteredRequests = make([]*LLMRequest, 0)

	for _, req := range m.requests {
		text := m.extractSearchableText(req)
		if strings.Contains(text, query) {
			m.filteredRequests = append(m.filteredRequests, req)
		}
	}
}

// getDisplayRequests returns the filtered and sorted list of requests for display
func (m *model) getDisplayRequests() []*LLMRequest {
	if m.searchQuery == "" {
		return m.getSortedRequests(m.requests)
	}
	return m.getSortedRequests(m.filteredRequests)
}

// getSortedRequests returns a sorted copy of requests based on current sort settings
func (m *model) getSortedRequests(reqs []*LLMRequest) []*LLMRequest {
	if len(reqs) == 0 {
		return reqs
	}

	// If no sort is active, return original order
	if m.sortField == SortByNone {
		return reqs
	}

	// Create a copy to avoid modifying the original
	sorted := make([]*LLMRequest, len(reqs))
	copy(sorted, reqs)

	// Sort using Go's sort.Slice for efficiency
	sort.Slice(sorted, func(i, j int) bool {
		var less bool
		switch m.sortField {
		case SortByID:
			less = sorted[i].ID < sorted[j].ID
		case SortByStatus:
			less = sorted[i].Status < sorted[j].Status
		case SortByModel:
			less = sorted[i].Model < sorted[j].Model
		case SortByCode:
			less = sorted[i].StatusCode < sorted[j].StatusCode
		case SortBySize:
			less = sorted[i].ResponseSize < sorted[j].ResponseSize
		case SortByDuration:
			less = sorted[i].Duration < sorted[j].Duration
		case SortByInputTokens:
			// Use actual tokens if available, otherwise estimated
			iTokens := sorted[i].InputTokens
			if iTokens == 0 {
				iTokens = sorted[i].EstimatedInputTokens
			}
			jTokens := sorted[j].InputTokens
			if jTokens == 0 {
				jTokens = sorted[j].EstimatedInputTokens
			}
			less = iTokens < jTokens
		case SortByOutputTokens:
			less = sorted[i].OutputTokens < sorted[j].OutputTokens
		case SortByCost:
			less = sorted[i].Cost < sorted[j].Cost
		default:
			less = sorted[i].ID < sorted[j].ID
		}

		if m.sortDirection == SortDesc {
			return !less
		}
		return less
	})

	return sorted
}

// toggleSort cycles through sort options for a field: ascending -> descending -> no sort -> ascending
func (m *model) toggleSort(field SortField) {
	if m.sortField == field {
		// Same field - cycle through: ascending -> descending -> no sort -> ascending
		if m.sortDirection == SortAsc {
			m.sortDirection = SortDesc
		} else {
			// Currently descending, go to no sort
			m.sortField = SortByNone
		}
	} else {
		// Different field (or no sort active) - start with ascending
		m.sortField = field
		m.sortDirection = SortAsc
	}
}
