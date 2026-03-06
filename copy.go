package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

func (m *model) copyActiveTab() {
	if m.selected == nil {
		m.copyMessage = "✗ No request selected"
		m.copyMessageTime = time.Now()
		return
	}

	text, label, err := m.getCopyText()
	if err != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", err.Error())
		m.copyMessageTime = time.Now()
		return
	}

	if err := clipboard.WriteAll(text); err != nil {
		m.copyMessage = fmt.Sprintf("✗ Clipboard error: %v", err)
		m.copyMessageTime = time.Now()
		return
	}

	m.copyMessage = fmt.Sprintf("Copied %s", label)
	m.copyMessageTime = time.Now()
}

func (m *model) copyInputOutput() {
	if m.selected == nil {
		m.copyMessage = "✗ No request selected"
		m.copyMessageTime = time.Now()
		return
	}

	var b strings.Builder

	// Request section: method, URL, headers, body
	b.WriteString("Input:\n")
	b.WriteString(fmt.Sprintf("%s %s HTTP/1.1\n", m.selected.Method, m.selected.Path))
	if m.selected.Host != "" {
		b.WriteString(fmt.Sprintf("Host: %s\n", m.selected.Host))
	}
	if len(m.selected.RequestHeaders) > 0 {
		headerKeys := make([]string, 0, len(m.selected.RequestHeaders))
		for k := range m.selected.RequestHeaders {
			if k == "Host" {
				continue
			}
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)
		for _, k := range headerKeys {
			values := m.selected.RequestHeaders[k]
			headerValue := strings.Join(values, ", ")
			if strings.ToLower(k) == "authorization" && len(headerValue) > 30 {
				headerValue = headerValue[:20] + "..." + headerValue[len(headerValue)-10:]
			}
			b.WriteString(fmt.Sprintf("%s: %s\n", k, headerValue))
		}
	}
	b.WriteString("\n")

	inputText, _, inputErr := rawBodyCopyText(m.selected.RequestBody, "input")
	if inputErr != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", inputErr.Error())
		m.copyMessageTime = time.Now()
		return
	}
	b.WriteString(inputText)

	// Response section: status, headers, body
	b.WriteString("\n\nOutput:\n")
	if m.selected.StatusCode > 0 {
		statusText := http.StatusText(m.selected.StatusCode)
		b.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\n", m.selected.StatusCode, statusText))
	}
	if len(m.selected.ResponseHeaders) > 0 {
		headerKeys := make([]string, 0, len(m.selected.ResponseHeaders))
		for k := range m.selected.ResponseHeaders {
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)
		for _, k := range headerKeys {
			values := m.selected.ResponseHeaders[k]
			b.WriteString(fmt.Sprintf("%s: %s\n", k, strings.Join(values, ", ")))
		}
	}
	b.WriteString("\n")

	outputText, _, outputErr := outputCopyText(m.selected)
	if outputErr != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", outputErr.Error())
		m.copyMessageTime = time.Now()
		return
	}
	b.WriteString(outputText)

	if err := clipboard.WriteAll(b.String()); err != nil {
		m.copyMessage = fmt.Sprintf("✗ Clipboard error: %v", err)
		m.copyMessageTime = time.Now()
		return
	}

	m.copyMessage = "Copied input+output"
	m.copyMessageTime = time.Now()
}

func (m *model) copySessionID() {
	if m.sessionID == "" {
		m.copyMessage = "✗ No session ID available"
		m.copyMessageTime = time.Now()
		return
	}
	if err := clipboard.WriteAll(m.sessionID); err != nil {
		m.copyMessage = fmt.Sprintf("✗ Clipboard error: %v", err)
		m.copyMessageTime = time.Now()
		return
	}
	m.copyMessage = fmt.Sprintf("Copied session ID %s", m.sessionID)
	m.copyMessageTime = time.Now()
}

func (m *model) getCopyText() (string, string, error) {
	switch m.activeTab {
	case TabOutput:
		return outputCopyText(m.selected)
	case TabRawInput:
		return rawBodyCopyText(m.selected.RequestBody, "raw input")
	case TabRawOutput:
		return rawBodyCopyText(m.selected.ResponseBody, "raw output")
	default:
		return "", "", fmt.Errorf("nothing to copy")
	}
}

func rawBodyCopyText(body []byte, label string) (string, string, error) {
	if len(body) == 0 {
		return "", label, fmt.Errorf("no %s", label)
	}

	// Truncate long base64 strings (e.g. images) to match the raw view display
	body = truncateLongBase64Strings(body)

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err == nil {
		return pretty.String(), label, nil
	}

	return string(body), label, nil
}

func outputCopyText(req *LLMRequest) (string, string, error) {
	if req == nil {
		return "", "output", fmt.Errorf("no request selected")
	}
	if len(req.ResponseBody) == 0 {
		if req.Status == StatusPending {
			return "", "output", fmt.Errorf("response pending")
		}
		return "", "output", fmt.Errorf("no output body")
	}

	if text := extractLLMOutputText(req); text != "" {
		return text, "output", nil
	}

	return rawBodyCopyText(req.ResponseBody, "output")
}

func extractLLMOutputText(req *LLMRequest) string {
	if req == nil || len(req.ResponseBody) == 0 {
		return ""
	}

	if isAnthropicEndpoint(req.Path) {
		return extractAnthropicOutputText(req.ResponseBody)
	}
	return extractOpenAIOutputText(req.ResponseBody)
}

func extractOpenAIOutputText(responseBody []byte) string {
	var resp OpenAIResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		if isSSEData(responseBody) {
			if assembled := reassembleSSEResponse(responseBody); assembled != nil {
				resp = *assembled
			} else {
				return ""
			}
		} else {
			return ""
		}
	}

	var parts []string
	for _, choice := range resp.Choices {
		choiceText := renderOpenAIChoiceCopyText(choice)
		if choiceText == "" {
			continue
		}
		if len(resp.Choices) > 1 {
			choiceText = fmt.Sprintf("Choice %d:\n%s", choice.Index, choiceText)
		}
		parts = append(parts, choiceText)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func renderOpenAIChoiceCopyText(choice OpenAIChoice) string {
	var parts []string

	if content := normalizeCopiedOutputText(extractOpenAITextContent(choice.Message.Content)); content != "" {
		parts = append(parts, content)
	}
	if reasoning := normalizeCopiedOutputText(choice.Message.ReasoningContent); reasoning != "" {
		parts = append(parts, reasoning)
	}
	if len(choice.Message.ToolCalls) > 0 {
		toolCallsJSON, err := json.MarshalIndent(choice.Message.ToolCalls, "", "  ")
		if err == nil {
			parts = append(parts, string(toolCallsJSON))
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func normalizeCopiedOutputText(text string) string {
	text = sanitizeForTerminal(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractAnthropicOutputText(responseBody []byte) string {
	var resp AnthropicResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return ""
	}

	var parts []string
	var toolUses []ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if text := normalizeCopiedOutputText(block.Text); text != "" {
				parts = append(parts, text)
			}
		case "thinking":
			if thinking := normalizeCopiedOutputText(block.Thinking); thinking != "" {
				parts = append(parts, thinking)
			}
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			toolUses = append(toolUses, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      block.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}

	if len(toolUses) > 0 {
		toolCallsJSON, err := json.MarshalIndent(toolUses, "", "  ")
		if err == nil {
			parts = append(parts, string(toolCallsJSON))
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
