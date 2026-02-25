package main

import (
	"encoding/json"
	"strings"
)

const (
	listPreviewMinWidth = 12
	listPreviewMaxWidth = 48
)

// getRequestPreviewSnippet returns a cached snippet used in the request list preview column.
func (m *model) getRequestPreviewSnippet(req *LLMRequest) string {
	if req == nil {
		return ""
	}
	if m.requestPreviewCache == nil {
		m.requestPreviewCache = make(map[int]string)
	}
	if cached, ok := m.requestPreviewCache[req.ID]; ok {
		return cached
	}
	snippet := extractRequestPreviewSnippet(req.Path, req.RequestBody)
	m.requestPreviewCache[req.ID] = snippet
	return snippet
}

func extractRequestPreviewSnippet(path string, requestBody []byte) string {
	if len(requestBody) == 0 {
		return ""
	}

	if isAnthropicEndpoint(path) {
		return extractAnthropicRequestPreviewSnippet(requestBody)
	}
	return extractOpenAIRequestPreviewSnippet(requestBody)
}

func extractAnthropicRequestPreviewSnippet(requestBody []byte) string {
	var req AnthropicRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return ""
	}

	if snippet := normalizePreviewSnippet(extractAnthropicTextContent(req.System)); snippet != "" {
		return snippet
	}

	for _, msg := range req.Messages {
		if snippet := normalizePreviewSnippet(extractAnthropicTextContent(msg.Content)); snippet != "" {
			return snippet
		}
	}

	return ""
}

func extractOpenAIRequestPreviewSnippet(requestBody []byte) string {
	var req OpenAIRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return ""
	}

	for _, msg := range req.Messages {
		if strings.EqualFold(msg.Role, "system") {
			if snippet := normalizePreviewSnippet(extractOpenAITextContent(msg.Content)); snippet != "" {
				return snippet
			}
		}
	}

	for _, msg := range req.Messages {
		if snippet := normalizePreviewSnippet(extractOpenAITextContent(msg.Content)); snippet != "" {
			return snippet
		}
	}

	return ""
}

func extractOpenAITextContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case map[string]interface{}:
		if text, ok := c["text"].(string); ok {
			return text
		}
	case []interface{}:
		var parts []string
		for _, part := range c {
			block, ok := part.(map[string]interface{})
			if !ok {
				continue
			}

			text, ok := block["text"].(string)
			if !ok || text == "" {
				continue
			}

			blockType, _ := block["type"].(string)
			if blockType == "" || blockType == "text" || blockType == "input_text" || blockType == "output_text" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func normalizePreviewSnippet(text string) string {
	text = sanitizeForTerminal(text)
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return strings.Join(strings.Fields(text), " ")
}

func truncateForColumn(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

// listPreviewColumnWidth calculates available width for the PREVIEW list column.
func (m model) listPreviewColumnWidth(baseRowWidth int) int {
	if m.width <= 0 {
		return 0
	}

	// Leave room for row prefix + list item padding.
	available := m.width - 6
	previewWidth := available - baseRowWidth
	if previewWidth < listPreviewMinWidth {
		return 0
	}
	if previewWidth > listPreviewMaxWidth {
		return listPreviewMaxWidth
	}
	return previewWidth
}
