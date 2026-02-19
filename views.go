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
	zone "github.com/lrstanley/bubblezone"
)

// extractAnthropicTextContent extracts text from Anthropic content (string or array of blocks)
func extractAnthropicTextContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, block := range c {
			if b, ok := block.(map[string]interface{}); ok {
				if t, _ := b["type"].(string); t == "text" {
					if text, ok := b["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// extractAnthropicToolUses extracts tool_use blocks as ToolCalls from Anthropic content
func extractAnthropicToolUses(content interface{}) []ToolCall {
	arr, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var toolCalls []ToolCall
	for _, block := range arr {
		b, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := b["type"].(string); t == "tool_use" {
			inputJSON, _ := json.Marshal(b["input"])
			toolCalls = append(toolCalls, ToolCall{
				ID:   fmt.Sprintf("%v", b["id"]),
				Type: "function",
				Function: ToolCallFunction{
					Name:      fmt.Sprintf("%v", b["name"]),
					Arguments: string(inputJSON),
				},
			})
		}
	}
	return toolCalls
}

// extractAnthropicToolResultID extracts the first tool_use_id from tool_result blocks
func extractAnthropicToolResultID(content interface{}) string {
	arr, ok := content.([]interface{})
	if !ok {
		return ""
	}
	for _, block := range arr {
		b, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := b["type"].(string); t == "tool_result" {
			if id, ok := b["tool_use_id"].(string); ok {
				return id
			}
		}
	}
	return ""
}

func (m model) renderSaveDialog() string {
	// Build the dialog content
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render("💾 Save Tape")

	prompt := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E2E8F0")).
		Render("Enter filename:")

	// Use the textinput's View method for proper cursor and input rendering
	input := m.saveInput.View()

	hint := lipgloss.NewStyle().
		Foreground(dimColor).
		Italic(true).
		Render("Press Enter to save, Esc to cancel")

	dialogContent := fmt.Sprintf("%s\n\n%s\n%s\n\n%s", title, prompt, input, hint)

	dialogBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(50).
		Align(lipgloss.Center)

	dialog := dialogBox.Render(dialogContent)

	// Use lipgloss.Place to center the dialog on the full screen
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		dialog,
	)
}

func (m *model) renderCostBreakdownPanel() string {
	displayRequests := m.getDisplayRequests()
	breakdown := AnalyzeRequestsCosts(displayRequests)

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render("Cost Breakdown")

	var b strings.Builder

	if breakdown.TotalRequests == 0 {
		b.WriteString(title)
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(dimColor).Render("No completed requests"))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render("Esc to close"))

		dialogBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1, 2).
			Width(60).
			Align(lipgloss.Center)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox.Render(b.String()))
	}

	b.WriteString(title)
	b.WriteString("\n\n")

	// By Model section
	sectionStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	b.WriteString(sectionStyle.Render("By Model"))
	b.WriteString("\n")

	headerStyle := lipgloss.NewStyle().Foreground(dimColor).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	costStyle := lipgloss.NewStyle().Foreground(successColor).Bold(true)

	// Sort models by cost descending
	var sortedModels []*ModelCostSummary
	for _, summary := range breakdown.Models {
		sortedModels = append(sortedModels, summary)
	}
	sort.Slice(sortedModels, func(i, j int) bool {
		return sortedModels[i].Cost > sortedModels[j].Cost
	})

	// Calculate column widths dynamically
	maxModelLen := 5 // minimum "MODEL"
	for _, s := range sortedModels {
		if len(s.Model) > maxModelLen {
			maxModelLen = len(s.Model)
		}
	}
	if maxModelLen > 30 {
		maxModelLen = 30
	}

	// Header row
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-*s %5s %10s %10s %10s",
		maxModelLen, "MODEL", "REQS", "IN TOK", "OUT TOK", "COST")))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render(
		"  " + strings.Repeat("─", maxModelLen+5+10+10+10+4)))
	b.WriteString("\n")

	for _, s := range sortedModels {
		modelName := s.Model
		if len(modelName) > maxModelLen {
			modelName = modelName[:maxModelLen-1] + "…"
		}
		line := fmt.Sprintf("  %-*s %5d %10s %10s ",
			maxModelLen, modelName, s.RequestCount,
			formatWithCommas(s.InputTokens), formatWithCommas(s.OutputTokens))
		b.WriteString(valueStyle.Render(line))
		b.WriteString(costStyle.Render(fmt.Sprintf("%10s", formatCost(s.Cost))))
		b.WriteString("\n")
	}

	// By Proxy section (only in multi-proxy mode)
	if m.isMultiProxy() {
		proxyCosts := AnalyzeProxyCosts(displayRequests)
		if len(proxyCosts) > 0 {
			b.WriteString("\n")
			b.WriteString(sectionStyle.Render("By Proxy"))
			b.WriteString("\n")

			maxProxyLen := 5
			for _, s := range proxyCosts {
				if len(s.Proxy) > maxProxyLen {
					maxProxyLen = len(s.Proxy)
				}
			}
			if maxProxyLen > 20 {
				maxProxyLen = 20
			}

			b.WriteString(headerStyle.Render(fmt.Sprintf("  %-*s %5s %10s %10s %10s",
				maxProxyLen, "PROXY", "REQS", "IN TOK", "OUT TOK", "COST")))
			b.WriteString("\n")
			b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render(
				"  " + strings.Repeat("─", maxProxyLen+5+10+10+10+4)))
			b.WriteString("\n")

			for _, s := range proxyCosts {
				proxyName := s.Proxy
				if len(proxyName) > maxProxyLen {
					proxyName = proxyName[:maxProxyLen-1] + "…"
				}
				line := fmt.Sprintf("  %-*s %5d %10s %10s ",
					maxProxyLen, proxyName, s.RequestCount,
					formatWithCommas(s.InputTokens), formatWithCommas(s.OutputTokens))
				b.WriteString(valueStyle.Render(line))
				b.WriteString(costStyle.Render(fmt.Sprintf("%10s", formatCost(s.Cost))))
				b.WriteString("\n")
			}
		}
	}

	// Totals
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render(
		"  " + strings.Repeat("━", 50)))
	b.WriteString("\n")
	totalLabelStyle := lipgloss.NewStyle().Foreground(primaryColor).Bold(true)
	totalValueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	totalCostStyle := lipgloss.NewStyle().Foreground(successColor).Bold(true)

	b.WriteString(totalLabelStyle.Render("  Requests: "))
	b.WriteString(totalValueStyle.Render(fmt.Sprintf("%d", breakdown.TotalRequests)))
	b.WriteString(totalLabelStyle.Render("   Input: "))
	b.WriteString(totalValueStyle.Render(formatWithCommas(breakdown.TotalInputTokens)))
	b.WriteString(totalLabelStyle.Render("   Output: "))
	b.WriteString(totalValueStyle.Render(formatWithCommas(breakdown.TotalOutputTokens)))
	b.WriteString("\n")
	b.WriteString(totalLabelStyle.Render("  Total Cost: "))
	b.WriteString(totalCostStyle.Render(formatCost(breakdown.TotalCost)))
	b.WriteString("\n\n")

	// Hint
	b.WriteString(lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render("c/Esc to close"))

	// Calculate dialog width based on content
	dialogWidth := maxModelLen + 5 + 10 + 10 + 10 + 4 + 8 // columns + padding
	if dialogWidth < 60 {
		dialogWidth = 60
	}
	if dialogWidth > m.width-4 {
		dialogWidth = m.width - 4
	}

	dialogBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(dialogWidth)

	dialog := dialogBox.Render(b.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *model) renderListView() string {
	var b strings.Builder

	// Header
	var header string
	var proxyInfo string

	if m.tapeMode {
		header = titleStyle.Render("📼 LLM Proxy - Tape Playback")
		proxyInfo = statusBarStyle.Render(fmt.Sprintf("Tape: %s", m.tape.FilePath))
	} else {
		header = titleStyle.Render("⚡ LLM Proxy")
		if m.listenAddr == "multi" {
			// Multi-proxy mode: targetURL contains the formatted proxy list
			proxyInfo = statusBarStyle.Render(m.targetURL)
		} else {
			proxyInfo = statusBarStyle.Render(fmt.Sprintf("%s → %s", m.listenAddr, m.targetURL))
		}
	}

	// Show recording indicator if saving to tape
	if m.saveTapeFile != "" {
		header = titleStyle.Render("⚡ LLM Proxy 🔴 REC")
	}

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, header, "  ", proxyInfo))
	b.WriteString("\n")

	// Search bar
	if m.searchMode {
		searchPrompt := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("/")
		searchInput := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0")).Render(m.searchQuery + "█")
		searchHint := lipgloss.NewStyle().Foreground(dimColor).Render(" (enter to confirm, esc to cancel)")
		b.WriteString(searchPrompt + searchInput + searchHint)
	} else if m.searchQuery != "" {
		// Show active search filter
		searchIndicator := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(
			fmt.Sprintf("🔍 \"%s\"", m.searchQuery))
		clearHint := lipgloss.NewStyle().Foreground(dimColor).Render(" (esc to clear)")
		b.WriteString(searchIndicator + clearHint)
	}
	b.WriteString("\n")

	// Column headers with sort indicators (clickable)
	b.WriteString(m.renderSortableHeaders())
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", m.width-2)))
	b.WriteString("\n")

	// Get filtered and sorted requests
	displayRequests := m.getDisplayRequests()

	// Request list (chronological: oldest at top, newest at bottom)
	listHeight := m.height - 9 // Adjusted for search bar
	start := 0
	end := len(displayRequests)

	// Scroll to keep cursor visible
	if len(displayRequests) > listHeight {
		if m.cursor >= listHeight {
			start = m.cursor - listHeight + 1
		}
		end = start + listHeight
		if end > len(displayRequests) {
			end = len(displayRequests)
		}
	}

	for i := start; i < end; i++ {
		req := displayRequests[i]
		row := m.renderRequestRow(req, i == m.cursor)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Fill empty space
	visibleCount := end - start
	for i := visibleCount; i < listHeight; i++ {
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

	// Show save message if present
	if m.saveMessage != "" {
		msgStyle := lipgloss.NewStyle().Foreground(successColor)
		if strings.HasPrefix(m.saveMessage, "✗") {
			msgStyle = lipgloss.NewStyle().Foreground(errorColor)
		}
		b.WriteString(msgStyle.Render(m.saveMessage))
		return b.String()
	}

	// Build help text based on mode
	var help string
	if m.tapeMode {
		// Tape playback help
		playState := "▶"
		if m.tapePlaying {
			playState = "⏸"
		}

		// Playback mode indicator
		modeStr := "STEP"
		if m.tapeRealtime {
			modeStr = "REAL"
		}
		playIndicator := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(
			fmt.Sprintf(" [%s %s %dx]", playState, modeStr, m.tapeSpeed))

		// Follow indicator
		followIndicator := ""
		if m.followLatest {
			followIndicator = lipgloss.NewStyle().Foreground(successColor).Render(" [FOLLOW]")
		}

		// Progress bar
		progress := 0.0
		if m.tape != nil {
			progress = m.tape.GetProgress()
		}
		progressBar := renderProgressBar(progress, 20)

		// Time display
		timeDisplay := ""
		if m.tape != nil {
			elapsed := m.tape.CurrentTime.Sub(m.tape.StartTime)
			total := m.tape.Duration
			timeDisplay = lipgloss.NewStyle().Foreground(dimColor).Render(
				fmt.Sprintf(" %s / %s", formatDuration(elapsed), formatDuration(total)))
		}

		// Mouse mode indicator
		mouseIndicator := ""
		if !m.mouseEnabled {
			mouseIndicator = lipgloss.NewStyle().Foreground(warningColor).Render(" [SELECT]")
		}

		help = helpStyle.Render("space play • / search • c cost • [/] step • -/+ speed • f follow • q quit") + playIndicator + followIndicator + mouseIndicator + " " + progressBar + timeDisplay
	} else {
		// Live mode help
		followIndicator := ""
		if m.followLatest {
			followIndicator = lipgloss.NewStyle().Foreground(successColor).Render(" [FOLLOW]")
		}
		numIndicator := ""
		if m.numBuffer != "" {
			numIndicator = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(fmt.Sprintf(" [%s]", m.numBuffer))
		}
		// Mouse mode indicator
		mouseIndicator := ""
		if !m.mouseEnabled {
			mouseIndicator = lipgloss.NewStyle().Foreground(warningColor).Render(" [SELECT]")
		}
		help = helpStyle.Render("↑/↓ nav • / search • enter select • c cost • g/G top/bot • f follow • s save • q quit") + followIndicator + numIndicator + mouseIndicator
	}

	// Calculate total cost across display requests
	totalCost := 0.0
	for _, req := range displayRequests {
		totalCost += req.Cost
	}

	// Build status with request count and total cost
	var statusText string
	if m.searchQuery != "" {
		// Show filtered count
		if totalCost > 0 {
			statusText = fmt.Sprintf("%d/%d requests • $%.4f", len(displayRequests), len(m.requests), totalCost)
		} else {
			statusText = fmt.Sprintf("%d/%d requests", len(displayRequests), len(m.requests))
		}
	} else {
		if totalCost > 0 {
			statusText = fmt.Sprintf("%d requests • $%.4f", len(m.requests), totalCost)
		} else {
			statusText = fmt.Sprintf("%d requests", len(m.requests))
		}
	}
	count := statusBarStyle.Render(statusText)
	footer := lipgloss.JoinHorizontal(lipgloss.Bottom, help, strings.Repeat(" ", max(0, m.width-lipgloss.Width(help)-lipgloss.Width(count)-2)), count)
	b.WriteString(footer)

	return zone.Scan(b.String())
}

// isMultiProxy returns true if running in multi-proxy mode
func (m *model) isMultiProxy() bool {
	return m.listenAddr == "multi"
}

// renderSortableHeaders renders clickable column headers with sort indicators
func (m *model) renderSortableHeaders() string {
	// Column widths (must match renderRequestRow)
	const (
		colID       = 6
		colStatus   = 12
		colProxy    = 12
		colModel    = 24
		colCode     = 6
		colSize     = 10
		colDuration = 12
		colInTok    = 10
		colOutTok   = 10
		colCost     = 10
	)

	// Helper to render a header with sort indicator
	renderHeader := func(label string, width int, field SortField, zoneID string) string {
		indicator := " "
		style := lipgloss.NewStyle().Foreground(dimColor).Bold(true)

		if m.sortField == field {
			style = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
			if m.sortDirection == SortAsc {
				indicator = "▲"
			} else {
				indicator = "▼"
			}
		}

		// Truncate label if needed to fit indicator
		maxLabelLen := width - 2
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen]
		}

		content := fmt.Sprintf("%-*s%s", width-1, label, indicator)
		return zone.Mark(zoneID, style.Render(content))
	}

	headers := []string{
		renderHeader("#", colID, SortByID, "sort-id"),
		renderHeader("STATUS", colStatus, SortByStatus, "sort-status"),
		renderHeader("MODEL", colModel, SortByModel, "sort-model"),
	}

	// Add PROXY column only in multi-proxy mode (after MODEL)
	if m.isMultiProxy() {
		headers = append(headers, renderHeader("PROXY", colProxy, SortByNone, "sort-proxy"))
	}

	headers = append(headers,
		renderHeader("CODE", colCode, SortByCode, "sort-code"),
		renderHeader("SIZE", colSize, SortBySize, "sort-size"),
		renderHeader("DURATION", colDuration, SortByDuration, "sort-duration"),
		renderHeader("IN TOK", colInTok, SortByInputTokens, "sort-intok"),
		renderHeader("OUT TOK", colOutTok, SortByOutputTokens, "sort-outtok"),
		renderHeader("COST", colCost, SortByCost, "sort-cost"),
	)

	row := lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(headers, " "))
	return row
}

// renderProgressBar renders a visual progress bar
func renderProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	empty := width - filled

	filledStyle := lipgloss.NewStyle().Foreground(primaryColor)
	emptyStyle := lipgloss.NewStyle().Foreground(borderColor)

	bar := filledStyle.Render(strings.Repeat("━", filled)) +
		emptyStyle.Render(strings.Repeat("─", empty))

	return lipgloss.NewStyle().Foreground(dimColor).Render("[") + bar + lipgloss.NewStyle().Foreground(dimColor).Render("]")
}

// formatDuration formats a duration for display
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%d:%02d", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%d:%02d:00", hours, mins)
}

func (m model) renderRequestRow(req *LLMRequest, selected bool) string {
	// Column widths (must match renderSortableHeaders)
	const (
		colID       = 6
		colStatus   = 12
		colProxy    = 12
		colModel    = 24
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
		if req.CachedResponse {
			statusText = "⚡ CACHED"
			statusStyle = lipgloss.NewStyle().Foreground(accentColor)
		} else {
			statusText = "✓  DONE"
			statusStyle = completeStyle
		}
	case StatusError:
		statusText = "✗  ERROR"
		statusStyle = errorStyle
	}
	statusStr := statusStyle.Render(fmt.Sprintf("%-*s", colStatus, statusText))

	// Proxy column (only in multi-proxy mode)
	var proxyStr string
	if m.isMultiProxy() {
		proxyName := req.ProxyName
		if proxyName == "" {
			proxyName = "-"
		}
		if len(proxyName) > colProxy-2 {
			proxyName = proxyName[:colProxy-3] + "…"
		}
		proxyStr = lipgloss.NewStyle().Foreground(accentColor).Render(fmt.Sprintf("%-*s", colProxy, proxyName))
	}

	// Model column
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

	// Duration column - show TTFT/total for streaming, total only for non-streaming
	durationText := "-"
	if req.Duration > 0 {
		if req.IsStreaming && req.TTFT > 0 {
			durationText = fmt.Sprintf("%s/%s", formatDuration(req.TTFT), formatDuration(req.Duration))
		} else {
			durationText = formatDuration(req.Duration)
		}
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
	var row string
	if m.isMultiProxy() {
		row = fmt.Sprintf("%s %s %s %s %s %s %s %s %s %s",
			idStr,
			statusStr,
			modelStr,
			proxyStr,
			codeStr,
			sizeStr,
			durationStr,
			inTokStr,
			outTokStr,
			costStr,
		)
	} else {
		row = fmt.Sprintf("%s %s %s %s %s %s %s %s %s",
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
	}

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

	// Proxy indicator (for multi-proxy mode)
	var proxyInfo string
	if m.selected.ProxyName != "" && m.selected.ProxyName != "default" {
		proxyInfo = lipgloss.NewStyle().Foreground(accentColor).Render("@" + m.selected.ProxyName)
	}

	// Cache indicator
	var cacheInfo string
	if m.selected.CachedResponse {
		cacheInfo = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("CACHED")
	}

	// Build cost/token info string
	var costInfo string
	if m.selected.Cost > 0 {
		costInfo = lipgloss.NewStyle().Foreground(successColor).Render(formatCost(m.selected.Cost))
	} else if m.selected.InputTokens > 0 || m.selected.OutputTokens > 0 {
		costInfo = lipgloss.NewStyle().Foreground(dimColor).Render("(no pricing)")
	}

	// Build timing info string
	var timingInfo string
	if m.selected.Duration > 0 {
		if m.selected.IsStreaming && m.selected.TTFT > 0 {
			timingInfo = lipgloss.NewStyle().Foreground(dimColor).Render(
				fmt.Sprintf("TTFT %s | Total %s", formatDuration(m.selected.TTFT), formatDuration(m.selected.Duration)))
		} else {
			timingInfo = lipgloss.NewStyle().Foreground(dimColor).Render(formatDuration(m.selected.Duration))
		}
	}

	// Build header line with all components
	headerParts := []string{header, "  ", modelInfo}
	if proxyInfo != "" {
		headerParts = append(headerParts, proxyInfo)
	}
	if cacheInfo != "" {
		headerParts = append(headerParts, "  ", cacheInfo)
	}
	if timingInfo != "" {
		headerParts = append(headerParts, "  ", timingInfo)
	}
	if costInfo != "" {
		headerParts = append(headerParts, "  ", costInfo)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, headerParts...))
	b.WriteString("\n\n")

	// Tabs
	tabs := []string{"Messages", "Output", "Raw Input", "Raw Output"}
	var tabRow []string
	for i, tab := range tabs {
		tabZoneID := fmt.Sprintf("tab-%d", i)
		var tabContent string
		if Tab(i) == m.activeTab {
			tabContent = activeTabStyle.Render(fmt.Sprintf("%d. %s", i+1, tab))
		} else {
			tabContent = inactiveTabStyle.Render(fmt.Sprintf("%d. %s", i+1, tab))
		}
		tabRow = append(tabRow, zone.Mark(tabZoneID, tabContent))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tabRow...))
	b.WriteString("\n\n")

	// Viewport content
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Footer - show context-sensitive help
	var help string
	if m.activeTab == TabMessages {
		help = helpStyle.Render("1-4/tab • J/K req • n/N msg • c/C collapse • click [Image] • g/G top/end • ↑/↓ scroll • esc back")
	} else if m.activeTab == TabOutput {
		help = helpStyle.Render("1-4/tab • J/K req • n/N msg • c copy • y copy both • g/G top/end • ↑/↓ scroll • esc back")
	} else {
		help = helpStyle.Render("1-4/tab • J/K req • c copy • y copy both • g/G top/end • ↑/↓ scroll • M select • esc/q back")
	}

	// Mouse mode indicator for detail view
	if !m.mouseEnabled {
		help += lipgloss.NewStyle().Foreground(warningColor).Render(" [SELECT]")
	}

	if m.copyMessage != "" {
		msgStyle := lipgloss.NewStyle().Foreground(successColor)
		if strings.HasPrefix(m.copyMessage, "✗") {
			msgStyle = lipgloss.NewStyle().Foreground(errorColor)
		}
		help += " " + msgStyle.Render(m.copyMessage)
	}

	// Build right side status
	var rightStatus string
	if (m.activeTab == TabMessages || m.activeTab == TabOutput) && len(m.messagePositions) > 0 {
		msgIndicator := lipgloss.NewStyle().Foreground(accentColor).Render(
			fmt.Sprintf("[%d/%d]", m.currentMsgIndex+1, len(m.messagePositions)))
		rightStatus = msgIndicator + " " + statusBarStyle.Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100)))
	} else {
		rightStatus = statusBarStyle.Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100)))
	}
	footer := lipgloss.JoinHorizontal(lipgloss.Bottom, help, strings.Repeat(" ", max(0, m.width-lipgloss.Width(help)-lipgloss.Width(rightStatus)-2)), rightStatus)
	b.WriteString(footer)

	return b.String()
}

func (m *model) renderTabContent() string {
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

func (m *model) renderMessagesTab() string {
	if len(m.selected.RequestBody) == 0 {
		return contentStyle.Render("No request body")
	}

	if isAnthropicEndpoint(m.selected.Path) {
		return m.renderAnthropicMessagesTab()
	}

	var req OpenAIRequest
	if err := json.Unmarshal(m.selected.RequestBody, &req); err != nil {
		return errorStyle.Render(fmt.Sprintf("Failed to parse request: %v", err))
	}

	var b strings.Builder
	lineCount := 0

	// Content width for boxes (viewport width minus border/padding)
	contentWidth := m.width - 10

	// Reset image references - we'll collect them as we render
	m.imageRefs = nil
	imageCounter := 0

	// Request metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %.1f\n%s %d\n%s %v",
		labelStyle.Render("Model:"), req.Model,
		labelStyle.Render("Endpoint:"), m.selected.Path,
		labelStyle.Render("Temperature:"), req.Temperature,
		labelStyle.Render("Max Tokens:"), req.MaxTokens,
		labelStyle.Render("Stream:"), req.Stream,
	)
	metaRendered := metaBox.Render(meta)
	b.WriteString(metaRendered)
	b.WriteString("\n\n")
	lineCount += strings.Count(metaRendered, "\n") + 2

	b.WriteString(labelStyle.Render("═══ Messages ═══"))
	b.WriteString("\n\n")
	lineCount += 2

	// Reset message positions
	m.messagePositions = make([]int, len(req.Messages))

	// Messages
	for i, msg := range req.Messages {
		// Track line position of this message
		m.messagePositions[i] = lineCount

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

		// Collapse indicator
		collapseIcon := "▼"
		if m.collapsedMessages[i] {
			collapseIcon = "▶"
		}

		// Highlight current message
		isCurrentMsg := i == m.currentMsgIndex
		collapseIndicator := lipgloss.NewStyle().
			Foreground(dimColor).
			Render(collapseIcon)
		if isCurrentMsg {
			collapseIndicator = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				Render(collapseIcon)
		}

		roleStyled := lipgloss.NewStyle().
			Foreground(roleColor).
			Bold(true).
			Render(fmt.Sprintf("%s %s", roleIcon, strings.ToUpper(msg.Role)))

		// Wrap only the header (collapse indicator + role) with zone.Mark for click detection
		// This ensures clicks work even when the message is partially scrolled
		msgZoneID := fmt.Sprintf("msg-%d", i)
		clickableHeader := zone.Mark(msgZoneID, collapseIndicator+" "+roleStyled)

		// Handle content which can be string or array
		content := ""
		switch c := msg.Content.(type) {
		case string:
			content = c
		case []interface{}:
			// Vision API format - arrays unmarshal as []interface{}
			var parts []string
			for _, part := range c {
				if p, ok := part.(map[string]interface{}); ok {
					if t, ok := p["type"].(string); ok {
						if t == "text" {
							if text, ok := p["text"].(string); ok {
								parts = append(parts, text)
							}
						} else if t == "image_url" {
							// Extract image URL and create a clickable reference
							imageCounter++
							if url, isBase64 := extractImageURL(p); url != "" {
								m.imageRefs = append(m.imageRefs, ImageRef{
									Index:    imageCounter,
									URL:      url,
									IsBase64: isBase64,
								})
							}
							// Use a plain text placeholder - will be styled after sanitization
							parts = append(parts, fmt.Sprintf("{IMG_PLACEHOLDER_%d}", imageCounter))
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

		// Border color based on role only
		borderColor := roleColor

		// Text width inside the box (minus border and padding)
		textWidth := contentWidth - 6

		var msgContent string
		var msgBox lipgloss.Style

		if m.collapsedMessages[i] {
			// Collapsed view - just show header with content preview
			preview := content
			// Replace image placeholders with simple text for preview
			for _, img := range m.imageRefs {
				placeholder := fmt.Sprintf("{IMG_PLACEHOLDER_%d}", img.Index)
				preview = strings.Replace(preview, placeholder, fmt.Sprintf("[Image %d]", img.Index), 1)
			}
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			// Replace newlines with spaces in preview
			preview = strings.ReplaceAll(preview, "\n", " ")
			previewStyled := lipgloss.NewStyle().
				Foreground(dimColor).
				Italic(true).
				Render(preview)

			msgContent = fmt.Sprintf("%s  %s", clickableHeader, previewStyled)

			msgBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderColor).
				Padding(0, 2).
				Width(contentWidth)
		} else {
			// Expanded view - full content
			msgBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderColor).
				Padding(0, 2).
				Width(contentWidth)

			renderedContent := m.replaceImagePlaceholders(renderContentSmart(content, textWidth))

			// Build reasoning block if present
			reasoningBlock := ""
			if msg.ReasoningContent != "" {
				reasoningLabel := lipgloss.NewStyle().
					Foreground(dimColor).
					Bold(true).
					Render("💭 THINKING")
				reasoningText := sanitizeForTerminal(msg.ReasoningContent)
				reasoningInnerBox := lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(dimColor).
					Padding(0, 2).
					Width(textWidth)
				reasoningBlock = reasoningInnerBox.Render(reasoningLabel+"\n\n"+renderMarkdown(reasoningText, textWidth-6))
			}

			headerText := clickableHeader
			if msg.Name != "" {
				headerText = fmt.Sprintf("%s (%s)", clickableHeader, msg.Name)
			}

			// Handle tool call ID for tool response messages
			if msg.ToolCallID != "" {
				toolCallLabel := lipgloss.NewStyle().
					Foreground(warningColor).
					Italic(true).
					Render(fmt.Sprintf("Response to: %s", msg.ToolCallID))
				if reasoningBlock != "" {
					msgContent = fmt.Sprintf("%s\n%s\n\n%s\n\n%s", headerText, toolCallLabel, reasoningBlock, renderedContent)
				} else {
					msgContent = fmt.Sprintf("%s\n%s\n\n%s", headerText, toolCallLabel, renderedContent)
				}
			} else if len(msg.ToolCalls) > 0 {
				msgContent = headerText + "\n\n"
				if reasoningBlock != "" {
					msgContent += reasoningBlock + "\n\n"
				}
				if content != "" && content != "null" {
					msgContent += renderedContent + "\n\n"
				}
				msgContent += m.renderToolCalls(msg.ToolCalls, textWidth)
			} else if reasoningBlock != "" {
				msgContent = fmt.Sprintf("%s\n\n%s\n\n%s", headerText, reasoningBlock, renderedContent)
			} else {
				msgContent = fmt.Sprintf("%s\n\n%s", headerText, renderedContent)
			}
		}

		rendered := msgBox.Render(msgContent)
		b.WriteString(rendered)
		lineCount += strings.Count(rendered, "\n") + 1

		if i < len(req.Messages)-1 {
			b.WriteString("\n")
			lineCount++
		}
	}

	return b.String()
}

// replaceImagePlaceholders replaces {IMG_PLACEHOLDER_N} with styled clickable image links
func (m *model) replaceImagePlaceholders(content string) string {
	for _, img := range m.imageRefs {
		placeholder := fmt.Sprintf("{IMG_PLACEHOLDER_%d}", img.Index)
		imgZoneID := fmt.Sprintf("img-%d", img.Index)
		imgStyle := lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true).
			Underline(true)
		styledPlaceholder := zone.Mark(imgZoneID, imgStyle.Render(fmt.Sprintf("[Image %d]", img.Index)))
		content = strings.Replace(content, placeholder, styledPlaceholder, 1)
	}
	return content
}

func (m *model) renderOutputTab() string {
	if len(m.selected.ResponseBody) == 0 {
		if m.selected.Status == StatusPending {
			return pendingStyle.Render("⏳ Waiting for response...")
		}
		return contentStyle.Render("No response body")
	}

	if isAnthropicEndpoint(m.selected.Path) {
		return m.renderAnthropicOutputTab()
	}

	var resp OpenAIResponse
	if err := json.Unmarshal(m.selected.ResponseBody, &resp); err != nil {
		// Try to show raw response if not standard OpenAI format
		return renderJSONBody(m.selected.ResponseBody, "Response")
	}

	var b strings.Builder
	lineCount := 0

	// Content width for boxes (viewport width minus border/padding)
	contentWidth := m.width - 10
	textWidth := contentWidth - 6

	// Response metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(successColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %d\n%s %d\n%s %d",
		labelStyle.Render("ID:"), resp.ID,
		labelStyle.Render("Model:"), resp.Model,
		labelStyle.Render("Prompt Tokens:"), resp.Usage.PromptTokens,
		labelStyle.Render("Completion Tokens:"), resp.Usage.CompletionTokens,
		labelStyle.Render("Total Tokens:"), resp.Usage.TotalTokens,
	)
	if m.selected.TTFT > 0 {
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("TTFT:"), formatDuration(m.selected.TTFT))
	}
	if m.selected.Duration > 0 {
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("Total Latency:"), formatDuration(m.selected.Duration))
	}
	metaRendered := metaBox.Render(meta)
	b.WriteString(metaRendered)
	b.WriteString("\n\n")
	lineCount += strings.Count(metaRendered, "\n") + 2

	b.WriteString(labelStyle.Render("═══ Choices ═══"))
	b.WriteString("\n\n")
	lineCount += 2

	// Reset message positions for output tab choices
	m.messagePositions = make([]int, len(resp.Choices))

	// Choices
	for i, choice := range resp.Choices {
		// Track line position of this choice
		m.messagePositions[i] = lineCount

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

		// Determine border color based on finish reason only
		borderCol := successColor
		if choice.FinishReason == "tool_calls" {
			borderCol = warningColor
		}

		finishInfo := lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true).
			Render(fmt.Sprintf("Finish reason: %s", choice.FinishReason))

		msgBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderCol).
			Padding(1, 2).
			Width(contentWidth)

		var msgContent string
		hasToolCalls := len(choice.Message.ToolCalls) > 0

		if hasToolCalls {
			// Tool call response
			msgContent = roleStyled + "\n\n"
			if content != "" && content != "null" && content != "\"\"" {
				msgContent += renderMarkdown(content, textWidth) + "\n\n"
			}
			msgContent += m.renderToolCalls(choice.Message.ToolCalls, textWidth)
			msgContent += "\n\n" + finishInfo
		} else {
			// Regular text response
			msgContent = fmt.Sprintf("%s\n\n%s\n\n%s", roleStyled, renderMarkdown(content, textWidth), finishInfo)
		}

		rendered := msgBox.Render(msgContent)
		b.WriteString(rendered)
		lineCount += strings.Count(rendered, "\n") + 1

		if i < len(resp.Choices)-1 {
			b.WriteString("\n")
			lineCount++
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

	// Content width accounting for viewport padding
	contentWidth := m.width - 8

	// Replace long base64 strings with truncated placeholders to prevent lag
	bodyToRender := truncateLongBase64Strings(m.selected.RequestBody)

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, bodyToRender, "", "  "); err != nil {
		b.WriteString(wrapText(sanitizeForTerminal(string(bodyToRender)), contentWidth))
	} else {
		b.WriteString(highlightJSONWithWidth(prettyJSON.String(), contentWidth))
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

	// Content width accounting for viewport padding
	contentWidth := m.width - 8

	// Replace long base64 strings with truncated placeholders to prevent lag
	bodyToRender := truncateLongBase64Strings(m.selected.ResponseBody)

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, bodyToRender, "", "  "); err != nil {
		b.WriteString(wrapText(sanitizeForTerminal(string(bodyToRender)), contentWidth))
	} else {
		b.WriteString(highlightJSONWithWidth(prettyJSON.String(), contentWidth))
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
func (m model) renderToolCalls(toolCalls []ToolCall, maxWidth int) string {
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

func (m *model) renderAnthropicMessagesTab() string {
	var req AnthropicRequest
	if err := json.Unmarshal(m.selected.RequestBody, &req); err != nil {
		return errorStyle.Render(fmt.Sprintf("Failed to parse request: %v", err))
	}

	var b strings.Builder
	lineCount := 0
	contentWidth := m.width - 10
	textWidth := contentWidth - 6
	m.imageRefs = nil

	// Request metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %d\n%s %v",
		labelStyle.Render("Model:"), req.Model,
		labelStyle.Render("Endpoint:"), m.selected.Path,
		labelStyle.Render("Max Tokens:"), req.MaxTokens,
		labelStyle.Render("Stream:"), req.Stream,
	)
	metaRendered := metaBox.Render(meta)
	b.WriteString(metaRendered)
	b.WriteString("\n\n")
	lineCount += strings.Count(metaRendered, "\n") + 2

	b.WriteString(labelStyle.Render("═══ Messages ═══"))
	b.WriteString("\n\n")
	lineCount += 2

	// Build display messages: system (optional) + conversation
	type displayMsg struct {
		role         string
		content      string
		toolCalls    []ToolCall
		toolResultID string
	}
	var msgs []displayMsg
	imageCounter := 0

	if req.System != nil {
		msgs = append(msgs, displayMsg{
			role:    "system",
			content: extractAnthropicTextContent(req.System),
		})
	}

	for _, msg := range req.Messages {
		toolCalls := extractAnthropicToolUses(msg.Content)
		toolResultID := extractAnthropicToolResultID(msg.Content)

		// Extract content with image handling
		var content string
		switch c := msg.Content.(type) {
		case string:
			content = c
		case []interface{}:
			var parts []string
			for _, block := range c {
				bl, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				blockType, _ := bl["type"].(string)
				switch blockType {
				case "text":
					if text, ok := bl["text"].(string); ok {
						parts = append(parts, text)
					}
				case "thinking":
					if thinking, ok := bl["thinking"].(string); ok {
						parts = append(parts, "💭 "+thinking)
					}
				case "redacted_thinking":
					parts = append(parts, "💭 [thinking redacted]")
				case "image":
					imageCounter++
					if url, isBase64 := extractAnthropicImageURL(bl); url != "" {
						m.imageRefs = append(m.imageRefs, ImageRef{
							Index:    imageCounter,
							URL:      url,
							IsBase64: isBase64,
						})
					}
					parts = append(parts, fmt.Sprintf("{IMG_PLACEHOLDER_%d}", imageCounter))
				case "tool_result":
					switch rc := bl["content"].(type) {
					case string:
						parts = append(parts, rc)
					case []interface{}:
						for _, rb := range rc {
							mp, ok := rb.(map[string]interface{})
							if !ok {
								continue
							}
							rbType, _ := mp["type"].(string)
							switch rbType {
							case "text":
								if text, ok := mp["text"].(string); ok {
									parts = append(parts, text)
								}
							case "image":
								imageCounter++
								if url, isBase64 := extractAnthropicImageURL(mp); url != "" {
									m.imageRefs = append(m.imageRefs, ImageRef{
										Index:    imageCounter,
										URL:      url,
										IsBase64: isBase64,
									})
								}
								parts = append(parts, fmt.Sprintf("{IMG_PLACEHOLDER_%d}", imageCounter))
							}
						}
					}
				}
			}
			content = strings.Join(parts, "\n")
		}

		msgs = append(msgs, displayMsg{
			role:         msg.Role,
			content:      content,
			toolCalls:    toolCalls,
			toolResultID: toolResultID,
		})
	}

	m.messagePositions = make([]int, len(msgs))

	for i, msg := range msgs {
		m.messagePositions[i] = lineCount

		roleColor := dimColor
		roleIcon := "💬"
		switch msg.role {
		case "system":
			roleColor = accentColor
			roleIcon = "⚙️"
		case "user":
			roleColor = primaryColor
			roleIcon = "👤"
			if msg.toolResultID != "" {
				roleColor = warningColor
				roleIcon = "🔧"
			}
		case "assistant":
			roleColor = successColor
			roleIcon = "🤖"
		}

		collapseIcon := "▼"
		if m.collapsedMessages[i] {
			collapseIcon = "▶"
		}
		isCurrentMsg := i == m.currentMsgIndex
		collapseStyle := lipgloss.NewStyle().Foreground(dimColor)
		if isCurrentMsg {
			collapseStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
		}
		collapseIndicator := collapseStyle.Render(collapseIcon)

		roleStyled := lipgloss.NewStyle().
			Foreground(roleColor).
			Bold(true).
			Render(fmt.Sprintf("%s %s", roleIcon, strings.ToUpper(msg.role)))

		msgZoneID := fmt.Sprintf("msg-%d", i)
		clickableHeader := zone.Mark(msgZoneID, collapseIndicator+" "+roleStyled)

		content := sanitizeForTerminal(msg.content)
		var msgContent string
		var msgBox lipgloss.Style

		if m.collapsedMessages[i] {
			preview := content
			// Replace image placeholders with simple text for preview
			for _, img := range m.imageRefs {
				placeholder := fmt.Sprintf("{IMG_PLACEHOLDER_%d}", img.Index)
				preview = strings.Replace(preview, placeholder, fmt.Sprintf("[Image %d]", img.Index), 1)
			}
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			previewStyled := lipgloss.NewStyle().
				Foreground(dimColor).
				Italic(true).
				Render(preview)
			msgContent = fmt.Sprintf("%s  %s", clickableHeader, previewStyled)
			msgBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(roleColor).
				Padding(0, 2).
				Width(contentWidth)
		} else {
			msgBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(roleColor).
				Padding(0, 2).
				Width(contentWidth)

			renderedContent := m.replaceImagePlaceholders(renderContentSmart(content, textWidth))

			if msg.toolResultID != "" {
				toolCallLabel := lipgloss.NewStyle().
					Foreground(warningColor).
					Italic(true).
					Render(fmt.Sprintf("Response to: %s", msg.toolResultID))
				msgContent = fmt.Sprintf("%s\n%s\n\n%s", clickableHeader, toolCallLabel, renderedContent)
			} else if len(msg.toolCalls) > 0 {
				msgContent = clickableHeader + "\n\n"
				if content != "" {
					msgContent += renderedContent + "\n\n"
				}
				msgContent += m.renderToolCalls(msg.toolCalls, textWidth)
			} else {
				msgContent = fmt.Sprintf("%s\n\n%s", clickableHeader, renderedContent)
			}
		}

		rendered := msgBox.Render(msgContent)
		b.WriteString(rendered)
		lineCount += strings.Count(rendered, "\n") + 1

		if i < len(msgs)-1 {
			b.WriteString("\n")
			lineCount++
		}
	}

	return b.String()
}

func (m *model) renderAnthropicOutputTab() string {
	if len(m.selected.ResponseBody) == 0 {
		if m.selected.Status == StatusPending {
			return pendingStyle.Render("⏳ Waiting for response...")
		}
		return contentStyle.Render("No response body")
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(m.selected.ResponseBody, &resp); err != nil {
		return renderJSONBody(m.selected.ResponseBody, "Response")
	}

	var b strings.Builder
	lineCount := 0
	contentWidth := m.width - 10
	textWidth := contentWidth - 6

	// Response metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(successColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %d\n%s %d\n%s %s",
		labelStyle.Render("ID:"), resp.ID,
		labelStyle.Render("Model:"), resp.Model,
		labelStyle.Render("Input Tokens:"), resp.Usage.InputTokens,
		labelStyle.Render("Output Tokens:"), resp.Usage.OutputTokens,
		labelStyle.Render("Stop Reason:"), resp.StopReason,
	)
	if m.selected.TTFT > 0 {
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("TTFT:"), formatDuration(m.selected.TTFT))
	}
	if m.selected.Duration > 0 {
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("Total Latency:"), formatDuration(m.selected.Duration))
	}
	metaRendered := metaBox.Render(meta)
	b.WriteString(metaRendered)
	b.WriteString("\n\n")
	lineCount += strings.Count(metaRendered, "\n") + 2

	b.WriteString(labelStyle.Render("═══ Response ═══"))
	b.WriteString("\n\n")
	lineCount += 2

	m.messagePositions = []int{lineCount}

	// Collect text, thinking, and tool use blocks
	var textParts []string
	var thinkingParts []string
	var toolUses []ToolCall
	hasRedactedThinking := false
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "thinking":
			if block.Thinking != "" {
				thinkingParts = append(thinkingParts, block.Thinking)
			}
		case "redacted_thinking":
			hasRedactedThinking = true
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

	roleStyled := lipgloss.NewStyle().
		Foreground(successColor).
		Bold(true).
		Render("🤖 ASSISTANT")

	borderCol := successColor
	if resp.StopReason == "tool_use" {
		borderCol = warningColor
	}

	finishInfo := lipgloss.NewStyle().
		Foreground(dimColor).
		Italic(true).
		Render(fmt.Sprintf("Stop reason: %s", resp.StopReason))

	msgBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderCol).
		Padding(1, 2).
		Width(contentWidth)

	var msgContent string
	msgContent = roleStyled + "\n\n"

	// Render thinking blocks (if any)
	if len(thinkingParts) > 0 {
		thinkingLabel := lipgloss.NewStyle().
			Foreground(dimColor).
			Bold(true).
			Render("💭 THINKING")
		thinkingText := sanitizeForTerminal(strings.Join(thinkingParts, "\n"))
		thinkingBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dimColor).
			Padding(0, 2).
			Width(textWidth)
		msgContent += thinkingBox.Render(thinkingLabel+"\n\n"+renderMarkdown(thinkingText, textWidth-6)) + "\n\n"
	}
	if hasRedactedThinking {
		redactedLabel := lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true).
			Render("💭 [thinking redacted]")
		msgContent += redactedLabel + "\n\n"
	}

	fullText := sanitizeForTerminal(strings.Join(textParts, "\n"))

	if len(toolUses) > 0 {
		if fullText != "" {
			msgContent += renderMarkdown(fullText, textWidth) + "\n\n"
		}
		msgContent += m.renderToolCalls(toolUses, textWidth)
		msgContent += "\n\n" + finishInfo
	} else {
		if fullText != "" {
			msgContent += renderMarkdown(fullText, textWidth) + "\n\n"
		}
		msgContent += finishInfo
	}

	rendered := msgBox.Render(msgContent)
	b.WriteString(rendered)

	return b.String()
}

// truncateID shortens long tool call IDs for display
func truncateID(id string) string {
	if len(id) <= 20 {
		return id
	}
	return id[:8] + "..." + id[len(id)-8:]
}
