package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m model) renderListView() string {
	var b strings.Builder

	// Header
	header := titleStyle.Render("⚡ LLM Proxy")
	proxyInfo := statusBarStyle.Render(fmt.Sprintf("Listening: %s → %s", m.listenAddr, m.targetURL))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, header, "  ", proxyInfo))
	b.WriteString("\n\n")

	// Column headers (must match widths in renderRequestRow)
	headerRow := lipgloss.NewStyle().
		Foreground(dimColor).
		Bold(true).
		Padding(0, 1).
		Render(fmt.Sprintf("%-6s %-12s %-26s %-6s %-10s %-12s %-10s %-10s %-10s",
			"#", "STATUS", "MODEL", "CODE", "SIZE", "DURATION", "IN TOK", "OUT TOK", "COST"))
	b.WriteString(headerRow)
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", m.width-2)))
	b.WriteString("\n")

	// Request list
	listHeight := m.height - 8
	start := 0
	if m.cursor >= listHeight {
		start = m.cursor - listHeight + 1
	}

	for i := start; i < len(m.requests) && i < start+listHeight; i++ {
		req := m.requests[i]
		row := m.renderRequestRow(req, i == m.cursor)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Fill empty space
	for i := len(m.requests); i < start+listHeight; i++ {
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")

	// Show command mode or number buffer
	if m.commandMode {
		cmdPrompt := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(fmt.Sprintf(":%s", m.commandBuffer))
		cmdHelp := lipgloss.NewStyle().Foreground(dimColor).Render(" (enter to jump, esc to cancel)")
		b.WriteString(cmdPrompt + cmdHelp)
		return b.String()
	}

	// Build help text with follow mode indicator and number buffer
	followIndicator := ""
	if m.followLatest {
		followIndicator = lipgloss.NewStyle().Foreground(successColor).Render(" [FOLLOW]")
	}
	numIndicator := ""
	if m.numBuffer != "" {
		numIndicator = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(fmt.Sprintf(" [%s]", m.numBuffer))
	}
	help := helpStyle.Render("↑/↓/j/k navigate • :N goto • g/G top/bottom • f follow • enter select • q quit") + followIndicator + numIndicator

	// Calculate total cost across all requests
	totalCost := 0.0
	for _, req := range m.requests {
		totalCost += req.Cost
	}

	// Build status with request count and total cost
	var statusText string
	if totalCost > 0 {
		statusText = fmt.Sprintf("%d requests • $%.4f", len(m.requests), totalCost)
	} else {
		statusText = fmt.Sprintf("%d requests", len(m.requests))
	}
	count := statusBarStyle.Render(statusText)
	footer := lipgloss.JoinHorizontal(lipgloss.Bottom, help, strings.Repeat(" ", max(0, m.width-lipgloss.Width(help)-lipgloss.Width(count)-2)), count)
	b.WriteString(footer)

	return b.String()
}

func (m model) renderRequestRow(req *LLMRequest, selected bool) string {
	// Column widths
	const (
		colID       = 6
		colStatus   = 12
		colModel    = 26
		colCode     = 6
		colSize     = 10
		colDuration = 12
		colInTok    = 10
		colOutTok   = 10
		colCost     = 10
	)

	// ID column
	idStr := fmt.Sprintf("%-*d", colID, req.ID)

	// Status column - pad first, then style
	var statusText string
	var statusStyle lipgloss.Style
	switch req.Status {
	case StatusPending:
		statusText = "●  PENDING"
		statusStyle = pendingStyle
	case StatusComplete:
		statusText = "✓  DONE"
		statusStyle = completeStyle
	case StatusError:
		statusText = "✗  ERROR"
		statusStyle = errorStyle
	}
	statusStr := statusStyle.Render(fmt.Sprintf("%-*s", colStatus, statusText))

	// Model column - pad first, then style
	modelName := req.Model
	if len(modelName) > colModel-2 {
		modelName = modelName[:colModel-3] + "…"
	}
	modelStr := modelBadgeStyle.Render(fmt.Sprintf("%-*s", colModel, modelName))

	// Code column - pad first, then style
	var codeText string
	var codeStyle lipgloss.Style
	if req.StatusCode > 0 {
		codeText = fmt.Sprintf("%d", req.StatusCode)
		if req.StatusCode >= 200 && req.StatusCode < 300 {
			codeStyle = completeStyle
		} else if req.StatusCode >= 400 {
			codeStyle = errorStyle
		} else {
			codeStyle = lipgloss.NewStyle().Foreground(warningColor)
		}
	} else {
		codeText = "-"
		codeStyle = lipgloss.NewStyle().Foreground(dimColor)
	}
	codeStr := codeStyle.Render(fmt.Sprintf("%-*s", colCode, codeText))

	// Size column (no styling needed)
	sizeText := "-"
	if req.ResponseSize > 0 {
		sizeText = formatBytes(req.ResponseSize)
	}
	sizeStr := fmt.Sprintf("%-*s", colSize, sizeText)

	// Duration column (no styling needed)
	durationText := "-"
	if req.Duration > 0 {
		durationText = fmt.Sprintf("%v", req.Duration.Round(time.Millisecond))
	}
	durationStr := fmt.Sprintf("%-*s", colDuration, durationText)

	// Input tokens column - show actual if available, otherwise estimated with ~
	var inTokText string
	if req.InputTokens > 0 {
		inTokText = formatTokenCount(req.InputTokens)
	} else if req.EstimatedInputTokens > 0 {
		inTokText = "~" + formatTokenCount(req.EstimatedInputTokens)
	} else {
		inTokText = "-"
	}
	inTokStr := fmt.Sprintf("%-*s", colInTok, inTokText)

	// Output tokens column
	var outTokText string
	if req.OutputTokens > 0 {
		outTokText = formatTokenCount(req.OutputTokens)
	} else {
		outTokText = "-"
	}
	outTokStr := fmt.Sprintf("%-*s", colOutTok, outTokText)

	// Cost column
	var costText string
	if req.Cost > 0 {
		costText = formatCost(req.Cost)
	} else {
		costText = "-"
	}
	costStr := fmt.Sprintf("%-*s", colCost, costText)

	// Build row with simple spacing
	row := fmt.Sprintf("%s %s %s %s %s %s %s %s %s",
		idStr,
		statusStr,
		modelStr,
		codeStr,
		sizeStr,
		durationStr,
		inTokStr,
		outTokStr,
		costStr,
	)

	if selected {
		return selectedItemStyle.Render("▶ " + row)
	}
	return itemStyle.Render("  " + row)
}

func (m model) renderDetailView() string {
	if m.selected == nil {
		return "No request selected"
	}

	var b strings.Builder

	// Header with request info
	header := titleStyle.Render(fmt.Sprintf("Request #%d", m.selected.ID))
	modelInfo := modelBadgeStyle.Render(m.selected.Model)

	// Build cost/token info string
	var costInfo string
	if m.selected.Cost > 0 {
		costInfo = lipgloss.NewStyle().Foreground(successColor).Render(formatCost(m.selected.Cost))
	} else if m.selected.InputTokens > 0 || m.selected.OutputTokens > 0 {
		costInfo = lipgloss.NewStyle().Foreground(dimColor).Render("(no pricing)")
	}

	if costInfo != "" {
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, header, "  ", modelInfo, "  ", costInfo))
	} else {
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, header, "  ", modelInfo))
	}
	b.WriteString("\n\n")

	// Tabs
	tabs := []string{"Messages", "Output", "Raw Input", "Raw Output"}
	var tabRow []string
	for i, tab := range tabs {
		if Tab(i) == m.activeTab {
			tabRow = append(tabRow, activeTabStyle.Render(fmt.Sprintf("%d. %s", i+1, tab)))
		} else {
			tabRow = append(tabRow, inactiveTabStyle.Render(fmt.Sprintf("%d. %s", i+1, tab)))
		}
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tabRow...))
	b.WriteString("\n\n")

	// Viewport content
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Footer
	help := helpStyle.Render("1-4/tab switch tabs • ↑/↓ scroll • esc/q back")
	scroll := statusBarStyle.Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100)))
	footer := lipgloss.JoinHorizontal(lipgloss.Bottom, help, strings.Repeat(" ", max(0, m.width-lipgloss.Width(help)-lipgloss.Width(scroll)-2)), scroll)
	b.WriteString(footer)

	return b.String()
}

func (m model) renderTabContent() string {
	if m.selected == nil {
		return ""
	}

	switch m.activeTab {
	case TabMessages:
		return m.renderMessagesTab()
	case TabOutput:
		return m.renderOutputTab()
	case TabRawInput:
		return m.renderRawRequest()
	case TabRawOutput:
		return m.renderRawResponse()
	}
	return ""
}

func (m model) renderMessagesTab() string {
	if len(m.selected.RequestBody) == 0 {
		return contentStyle.Render("No request body")
	}

	var req OpenAIRequest
	if err := json.Unmarshal(m.selected.RequestBody, &req); err != nil {
		return errorStyle.Render(fmt.Sprintf("Failed to parse request: %v", err))
	}

	var b strings.Builder

	// Request metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 2).
		MarginBottom(1)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %.1f\n%s %d\n%s %v",
		labelStyle.Render("Model:"), req.Model,
		labelStyle.Render("Endpoint:"), m.selected.Path,
		labelStyle.Render("Temperature:"), req.Temperature,
		labelStyle.Render("Max Tokens:"), req.MaxTokens,
		labelStyle.Render("Stream:"), req.Stream,
	)
	b.WriteString(metaBox.Render(meta))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("═══ Messages ═══"))
	b.WriteString("\n\n")

	// Messages
	for i, msg := range req.Messages {
		roleColor := dimColor
		roleIcon := "💬"
		switch msg.Role {
		case "system":
			roleColor = accentColor
			roleIcon = "⚙️"
		case "user":
			roleColor = primaryColor
			roleIcon = "👤"
		case "assistant":
			roleColor = successColor
			roleIcon = "🤖"
		case "function", "tool":
			roleColor = warningColor
			roleIcon = "🔧"
		}

		roleStyled := lipgloss.NewStyle().
			Foreground(roleColor).
			Bold(true).
			Render(fmt.Sprintf("%s %s", roleIcon, strings.ToUpper(msg.Role)))

		// Handle content which can be string or array
		content := ""
		switch c := msg.Content.(type) {
		case string:
			content = c
		case []interface{}:
			// Vision API format
			var parts []string
			for _, part := range c {
				if p, ok := part.(map[string]interface{}); ok {
					if t, ok := p["type"].(string); ok {
						if t == "text" {
							if text, ok := p["text"].(string); ok {
								parts = append(parts, text)
							}
						} else if t == "image_url" {
							parts = append(parts, "[Image]")
						}
					}
				}
			}
			content = strings.Join(parts, "\n")
		default:
			contentBytes, _ := json.Marshal(msg.Content)
			content = string(contentBytes)
		}

		// Sanitize content for terminal
		content = sanitizeForTerminal(content)

		msgBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(roleColor).
			Padding(1, 2).
			Width(m.width - 8)

		msgContent := fmt.Sprintf("%s\n\n%s", roleStyled, wrapText(content, m.width-14))

		if msg.Name != "" {
			msgContent = fmt.Sprintf("%s (%s)\n\n%s", roleStyled, msg.Name, wrapText(content, m.width-14))
		}

		// Handle tool call ID for tool response messages
		if msg.ToolCallID != "" {
			toolCallLabel := lipgloss.NewStyle().
				Foreground(warningColor).
				Italic(true).
				Render(fmt.Sprintf("Response to: %s", msg.ToolCallID))
			msgContent = fmt.Sprintf("%s\n%s\n\n%s", roleStyled, toolCallLabel, wrapText(content, m.width-14))
		}

		// Handle tool calls in message
		if len(msg.ToolCalls) > 0 {
			msgContent = roleStyled + "\n\n"
			if content != "" && content != "null" {
				msgContent += wrapText(content, m.width-14) + "\n\n"
			}
			msgContent += m.renderToolCalls(msg.ToolCalls)
		}

		b.WriteString(msgBox.Render(msgContent))
		if i < len(req.Messages)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m model) renderOutputTab() string {
	if len(m.selected.ResponseBody) == 0 {
		if m.selected.Status == StatusPending {
			return pendingStyle.Render("⏳ Waiting for response...")
		}
		return contentStyle.Render("No response body")
	}

	var resp OpenAIResponse
	if err := json.Unmarshal(m.selected.ResponseBody, &resp); err != nil {
		// Try to show raw response if not standard OpenAI format
		return renderJSONBody(m.selected.ResponseBody, "Response")
	}

	var b strings.Builder

	// Response metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(successColor).
		Padding(0, 2).
		MarginBottom(1)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %d\n%s %d\n%s %d",
		labelStyle.Render("ID:"), resp.ID,
		labelStyle.Render("Model:"), resp.Model,
		labelStyle.Render("Prompt Tokens:"), resp.Usage.PromptTokens,
		labelStyle.Render("Completion Tokens:"), resp.Usage.CompletionTokens,
		labelStyle.Render("Total Tokens:"), resp.Usage.TotalTokens,
	)
	b.WriteString(metaBox.Render(meta))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("═══ Choices ═══"))
	b.WriteString("\n\n")

	// Choices
	for i, choice := range resp.Choices {
		roleStyled := lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true).
			Render(fmt.Sprintf("🤖 ASSISTANT (Choice %d)", choice.Index))

		content := ""
		switch c := choice.Message.Content.(type) {
		case string:
			content = c
		default:
			contentBytes, _ := json.Marshal(choice.Message.Content)
			content = string(contentBytes)
		}

		// Sanitize content for terminal
		content = sanitizeForTerminal(content)

		// Determine border color based on finish reason
		borderColor := successColor
		if choice.FinishReason == "tool_calls" {
			borderColor = warningColor
		}

		finishInfo := lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true).
			Render(fmt.Sprintf("Finish reason: %s", choice.FinishReason))

		msgBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2).
			Width(m.width - 8)

		var msgContent string
		hasToolCalls := len(choice.Message.ToolCalls) > 0

		if hasToolCalls {
			// Tool call response
			msgContent = roleStyled + "\n\n"
			if content != "" && content != "null" && content != "\"\"" {
				msgContent += wrapText(content, m.width-14) + "\n\n"
			}
			msgContent += m.renderToolCalls(choice.Message.ToolCalls)
			msgContent += "\n\n" + finishInfo
		} else {
			// Regular text response
			msgContent = fmt.Sprintf("%s\n\n%s\n\n%s", roleStyled, wrapText(content, m.width-14), finishInfo)
		}

		b.WriteString(msgBox.Render(msgContent))

		if i < len(resp.Choices)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m model) renderRawRequest() string {
	var b strings.Builder

	// HTTP Request Line
	requestLine := lipgloss.NewStyle().
		Foreground(primaryColor).
		Bold(true).
		Render(fmt.Sprintf("%s %s HTTP/1.1", m.selected.Method, m.selected.Path))
	b.WriteString(requestLine)
	b.WriteString("\n")

	// Host header
	hostLine := fmt.Sprintf("%s %s",
		lipgloss.NewStyle().Foreground(accentColor).Render("Host:"),
		m.selected.Host)
	b.WriteString(hostLine)
	b.WriteString("\n")

	// Request Headers
	if len(m.selected.RequestHeaders) > 0 {
		// Sort headers for consistent display
		headerKeys := make([]string, 0, len(m.selected.RequestHeaders))
		for k := range m.selected.RequestHeaders {
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)

		for _, k := range headerKeys {
			if k == "Host" {
				continue // Already shown above
			}
			values := m.selected.RequestHeaders[k]
			headerName := lipgloss.NewStyle().Foreground(accentColor).Render(k + ":")
			headerValue := strings.Join(values, ", ")
			// Truncate very long header values (like Authorization)
			if len(headerValue) > 80 {
				if strings.ToLower(k) == "authorization" {
					// Mask auth tokens
					headerValue = headerValue[:20] + "..." + headerValue[len(headerValue)-10:]
				} else {
					headerValue = headerValue[:77] + "..."
				}
			}
			b.WriteString(fmt.Sprintf("%s %s\n", headerName, headerValue))
		}
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", 40)))
	b.WriteString("\n\n")

	// Request Body
	if len(m.selected.RequestBody) == 0 {
		b.WriteString(contentStyle.Render("(no body)"))
		return b.String()
	}

	b.WriteString(labelStyle.Render(fmt.Sprintf("═══ Body (%s) ═══", formatBytes(len(m.selected.RequestBody)))))
	b.WriteString("\n\n")

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, m.selected.RequestBody, "", "  "); err != nil {
		b.WriteString(sanitizeForTerminal(string(m.selected.RequestBody)))
	} else {
		b.WriteString(highlightJSON(prettyJSON.String()))
	}

	return b.String()
}

func (m model) renderRawResponse() string {
	if m.selected.Status == StatusPending {
		return pendingStyle.Render("⏳ Waiting for response...")
	}

	var b strings.Builder

	// HTTP Status Line
	statusText := http.StatusText(m.selected.StatusCode)
	statusColor := successColor
	if m.selected.StatusCode >= 400 {
		statusColor = errorColor
	} else if m.selected.StatusCode >= 300 {
		statusColor = warningColor
	}

	statusLine := fmt.Sprintf("%s %s",
		lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(fmt.Sprintf("HTTP/1.1 %d", m.selected.StatusCode)),
		lipgloss.NewStyle().Foreground(statusColor).Render(statusText))
	b.WriteString(statusLine)
	b.WriteString("\n")

	// Response Headers
	if len(m.selected.ResponseHeaders) > 0 {
		headerKeys := make([]string, 0, len(m.selected.ResponseHeaders))
		for k := range m.selected.ResponseHeaders {
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)

		for _, k := range headerKeys {
			values := m.selected.ResponseHeaders[k]
			headerName := lipgloss.NewStyle().Foreground(accentColor).Render(k + ":")
			headerValue := strings.Join(values, ", ")
			if len(headerValue) > 80 {
				headerValue = headerValue[:77] + "..."
			}
			b.WriteString(fmt.Sprintf("%s %s\n", headerName, headerValue))
		}
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", 40)))
	b.WriteString("\n\n")

	// Response Body
	if len(m.selected.ResponseBody) == 0 {
		b.WriteString(contentStyle.Render("(no body)"))
		return b.String()
	}

	b.WriteString(labelStyle.Render(fmt.Sprintf("═══ Body (%s) ═══", formatBytes(len(m.selected.ResponseBody)))))
	b.WriteString("\n\n")

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, m.selected.ResponseBody, "", "  "); err != nil {
		b.WriteString(sanitizeForTerminal(string(m.selected.ResponseBody)))
	} else {
		b.WriteString(highlightJSON(prettyJSON.String()))
	}

	return b.String()
}

// renderJSONBody is a helper to render JSON body with syntax highlighting
func renderJSONBody(data []byte, title string) string {
	if len(data) == 0 {
		return contentStyle.Render(fmt.Sprintf("No %s data", strings.ToLower(title)))
	}

	var b strings.Builder
	b.WriteString(labelStyle.Render(fmt.Sprintf("═══ %s JSON (%s) ═══", title, formatBytes(len(data)))))
	b.WriteString("\n\n")

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, data, "", "  "); err != nil {
		b.WriteString(sanitizeForTerminal(string(data)))
	} else {
		b.WriteString(highlightJSON(prettyJSON.String()))
	}

	return b.String()
}

// renderToolCalls renders tool calls in a visually appealing format
func (m model) renderToolCalls(toolCalls []ToolCall) string {
	var b strings.Builder

	toolCallHeader := lipgloss.NewStyle().
		Foreground(warningColor).
		Bold(true).
		Render(fmt.Sprintf("🔧 TOOL CALLS (%d)", len(toolCalls)))
	b.WriteString(toolCallHeader)
	b.WriteString("\n")

	for i, tc := range toolCalls {
		// Tool call header with function name
		funcNameStyle := lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true)

		idStyle := lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true)

		b.WriteString("\n")
		b.WriteString(funcNameStyle.Render(fmt.Sprintf("▸ %s", tc.Function.Name)))
		if tc.ID != "" {
			b.WriteString("  ")
			b.WriteString(idStyle.Render(fmt.Sprintf("id: %s", truncateID(tc.ID))))
		}
		b.WriteString("\n")

		// Show arguments as JSON if parseable
		if tc.Function.Arguments != "" {
			argsLabel := lipgloss.NewStyle().
				Foreground(accentColor).
				Render("  Arguments:")
			b.WriteString(argsLabel)
			b.WriteString("\n")

			// Try to pretty-print as JSON
			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, []byte(tc.Function.Arguments), "    ", "  "); err == nil {
				b.WriteString("    ")
				b.WriteString(highlightJSON(prettyJSON.String()))
				b.WriteString("\n")
			} else {
				// Just show raw arguments if not valid JSON
				b.WriteString(fmt.Sprintf("    %s\n", sanitizeForTerminal(tc.Function.Arguments)))
			}
		}

		if i < len(toolCalls)-1 {
			b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render("  ─────────────────────────"))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// truncateID shortens long tool call IDs for display
func truncateID(id string) string {
	if len(id) <= 20 {
		return id
	}
	return id[:8] + "..." + id[len(id)-8:]
}
