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
		Foreground(textColor).
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
	valueStyle := lipgloss.NewStyle().Foreground(textColor)
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
	totalValueStyle := lipgloss.NewStyle().Foreground(textColor)
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
	var sessionInfo string

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

	if !m.tapeMode && m.sessionID != "" {
		sessionInfo = statusBarStyle.Render(fmt.Sprintf("session %s", m.sessionID))
	}

	headerParts := []string{header, "  ", proxyInfo}
	if sessionInfo != "" {
		headerParts = append(headerParts, "  ", sessionInfo)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, headerParts...))
	b.WriteString("\n")

	// Search bar
	if m.searchMode {
		searchPrompt := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("/")
		searchInput := lipgloss.NewStyle().Foreground(textColor).Render(m.searchQuery + "█")
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
	if m.copyMessage != "" {
		msgStyle := lipgloss.NewStyle().Foreground(successColor)
		if strings.HasPrefix(m.copyMessage, "✗") {
			msgStyle = lipgloss.NewStyle().Foreground(errorColor)
		}
		b.WriteString(msgStyle.Render(m.copyMessage))
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
		help = helpStyle.Render("↑/↓ nav • / search • enter select • c cost • g/G top/bot • f follow • s save • Y copy-session • q quit") + followIndicator + numIndicator + mouseIndicator
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
func (m model) isMultiProxy() bool {
	return m.listenAddr == "multi"
}

const (
	listColID       = 6
	listColStatus   = 12
	listColProxy    = 12
	listColModel    = 24
	listColCode     = 6
	listColSize     = 10
	listColDuration = 12
	listColInTok    = 10
	listColOutTok   = 10
	listColCost     = 10
)

type listColumns struct {
	id       int
	status   int
	proxy    int
	model    int
	code     int
	size     int
	duration int
	inTok    int
	outTok   int
	cost     int
	preview  int
}

func (m model) listViewColumns() listColumns {
	cols := listColumns{
		id:       listColID,
		status:   listColStatus,
		proxy:    listColProxy,
		model:    listColModel,
		code:     listColCode,
		size:     listColSize,
		duration: listColDuration,
		inTok:    listColInTok,
		outTok:   listColOutTok,
		cost:     listColCost,
	}

	// Base row width includes all fixed columns + spaces between columns.
	baseRowWidth := cols.id + cols.status + cols.model + cols.code + cols.size +
		cols.duration + cols.inTok + cols.outTok + cols.cost + 8
	if m.isMultiProxy() {
		baseRowWidth += cols.proxy + 1
	}
	cols.preview = m.listPreviewColumnWidth(baseRowWidth)
	return cols
}

// renderSortableHeaders renders clickable column headers with sort indicators
func (m *model) renderSortableHeaders() string {
	cols := m.listViewColumns()

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
		if maxLabelLen < 1 {
			maxLabelLen = 1
		}
		if len([]rune(label)) > maxLabelLen {
			label = truncateForColumn(label, maxLabelLen)
		}

		content := fmt.Sprintf("%-*s%s", width-1, label, indicator)
		return zone.Mark(zoneID, style.Render(content))
	}

	headers := []string{
		renderHeader("#", cols.id, SortByID, "sort-id"),
		renderHeader("STATUS", cols.status, SortByStatus, "sort-status"),
		renderHeader("MODEL", cols.model, SortByModel, "sort-model"),
	}

	// Add PROXY column only in multi-proxy mode (after MODEL)
	if m.isMultiProxy() {
		headers = append(headers, renderHeader("PROXY", cols.proxy, SortByNone, "sort-proxy"))
	}

	// Add request preview snippet column if there is enough width.
	if cols.preview > 0 {
		previewHeader := lipgloss.NewStyle().
			Foreground(dimColor).
			Bold(true).
			Render(fmt.Sprintf("%-*s", cols.preview, "PREVIEW"))
		headers = append(headers, previewHeader)
	}

	headers = append(headers,
		renderHeader("CODE", cols.code, SortByCode, "sort-code"),
		renderHeader("SIZE", cols.size, SortBySize, "sort-size"),
		renderHeader("DURATION", cols.duration, SortByDuration, "sort-duration"),
		renderHeader("IN TOK", cols.inTok, SortByInputTokens, "sort-intok"),
		renderHeader("OUT TOK", cols.outTok, SortByOutputTokens, "sort-outtok"),
		renderHeader("COST", cols.cost, SortByCost, "sort-cost"),
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

func (m *model) renderRequestRow(req *LLMRequest, selected bool) string {
	cols := m.listViewColumns()

	// ID column
	idStr := fmt.Sprintf("%-*d", cols.id, req.ID)

	// Status column - pad first, then style
	var statusText string
	var statusStyle lipgloss.Style
	switch req.Status {
	case StatusPending:
		if req.IsStreaming && len(req.ResponseBody) > 0 {
			statusText = "↻ STREAMING"
		} else {
			statusText = "●  PENDING"
		}
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
		if req.StatusCode == 499 {
			statusText = "✗  CANCELED"
			statusStyle = lipgloss.NewStyle().Foreground(warningColor)
		} else {
			statusText = "✗  ERROR"
			statusStyle = errorStyle
		}
	}
	statusStr := statusStyle.Render(fmt.Sprintf("%-*s", cols.status, statusText))

	// Proxy column (only in multi-proxy mode)
	var proxyStr string
	if m.isMultiProxy() {
		proxyName := req.ProxyName
		if proxyName == "" {
			proxyName = "-"
		}
		proxyName = truncateForColumn(proxyName, cols.proxy)
		proxyStr = lipgloss.NewStyle().Foreground(accentColor).Render(fmt.Sprintf("%-*s", cols.proxy, proxyName))
	}

	// Model column
	modelName := truncateForColumn(req.Model, cols.model)
	modelStr := modelBadgeStyle.Render(fmt.Sprintf("%-*s", cols.model, modelName))

	// Preview column (system prompt or first message snippet)
	var previewStr string
	if cols.preview > 0 {
		preview := m.getRequestPreviewSnippet(req)
		if preview == "" {
			preview = "-"
		}
		preview = truncateForColumn(preview, cols.preview)
		previewStr = lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true).
			Render(fmt.Sprintf("%-*s", cols.preview, preview))
	}

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
	codeStr := codeStyle.Render(fmt.Sprintf("%-*s", cols.code, codeText))

	// Size column (show live bytes while streaming)
	sizeText := "-"
	if req.ResponseSize > 0 {
		sizeText = formatBytes(req.ResponseSize)
	} else if req.Status == StatusPending && len(req.ResponseBody) > 0 {
		sizeText = formatBytes(len(req.ResponseBody))
	}
	sizeStr := fmt.Sprintf("%-*s", cols.size, sizeText)

	// Duration column - show TTFT/total for streaming, total only for non-streaming
	durationText := "-"
	if req.Duration > 0 {
		if req.IsStreaming && req.TTFT > 0 {
			durationText = fmt.Sprintf("%s/%s", formatDuration(req.TTFT), formatDuration(req.Duration))
		} else {
			durationText = formatDuration(req.Duration)
		}
	} else if req.Status == StatusPending {
		elapsed := time.Since(req.StartTime)
		if req.IsStreaming && req.TTFT > 0 {
			durationText = fmt.Sprintf("%s/%s", formatDuration(req.TTFT), formatDuration(elapsed))
		} else if elapsed > 0 {
			durationText = formatDuration(elapsed)
		}
	}
	durationStr := fmt.Sprintf("%-*s", cols.duration, durationText)

	// Input tokens column - show actual if available, otherwise estimated with ~
	var inTokText string
	if req.InputTokens > 0 {
		inTokText = formatTokenCount(req.InputTokens)
	} else if req.EstimatedInputTokens > 0 {
		inTokText = "~" + formatTokenCount(req.EstimatedInputTokens)
	} else {
		inTokText = "-"
	}
	inTokStr := fmt.Sprintf("%-*s", cols.inTok, inTokText)

	// Output tokens column
	var outTokText string
	if req.OutputTokens > 0 {
		outTokText = formatTokenCount(req.OutputTokens)
	} else {
		outTokText = "-"
	}
	outTokStr := fmt.Sprintf("%-*s", cols.outTok, outTokText)

	// Cost column
	var costText string
	if req.Cost > 0 {
		costText = formatCost(req.Cost)
	} else {
		costText = "-"
	}
	costStr := fmt.Sprintf("%-*s", cols.cost, costText)

	// Build row with simple spacing
	var row string
	if m.isMultiProxy() {
		row = fmt.Sprintf("%s %s %s %s",
			idStr,
			statusStr,
			modelStr,
			proxyStr,
		)
	} else {
		row = fmt.Sprintf("%s %s %s",
			idStr,
			statusStr,
			modelStr,
		)
	}

	if cols.preview > 0 {
		row += " " + previewStr
	}
	row = fmt.Sprintf("%s %s %s %s %s %s",
		row,
		codeStr,
		sizeStr,
		durationStr,
		inTokStr,
		outTokStr,
	)
	row += " " + costStr

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
	b.WriteString("\n")

	// Show cancel reason banner for 499 errors
	if m.selected.StatusCode == 499 && m.selected.CancelReason != "" {
		cancelBanner := lipgloss.NewStyle().
			Foreground(warningColor).
			Italic(true).
			Render("⚠ 499 Client Closed: " + m.selected.CancelReason)
		b.WriteString(cancelBanner)
		b.WriteString("\n")
	}
	b.WriteString("\n")

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
		help = helpStyle.Render("1-4/tab • J/K req • n/N msg • c/C collapse • click [Image] • e export • g/G top/end • ↑/↓ scroll • esc back")
	} else if m.activeTab == TabOutput {
		help = helpStyle.Render("1-4/tab • J/K req • n/N msg • c copy • y copy both • e export • g/G top/end • ↑/↓ scroll • esc back")
	} else {
		help = helpStyle.Render("1-4/tab • J/K req • c copy • y copy both • e export • g/G top/end • ↑/↓ scroll • M select • esc/q back")
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

func formatMessagesInputTokenCount(req *LLMRequest) string {
	if req == nil {
		return "-"
	}
	if req.InputTokens > 0 {
		return formatWithCommas(req.InputTokens)
	}
	if req.EstimatedInputTokens > 0 {
		return "~" + formatWithCommas(req.EstimatedInputTokens) + " (estimated)"
	}
	return "-"
}

func (m *model) renderMessagesTab() string {
	if len(m.selected.RequestBody) == 0 {
		return contentStyle.Render("No request body")
	}

	if isAnthropicEndpoint(m.selected.Path) {
		return m.renderAnthropicMessagesTab()
	}

	if isGeminiEndpoint(m.selected.Path) {
		return m.renderGeminiMessagesTab()
	}

	var req OpenAIRequest
	if err := json.Unmarshal(m.selected.RequestBody, &req); err != nil {
		return errorStyle.Render(fmt.Sprintf("Failed to parse request: %v", err))
	}

	var b strings.Builder
	lineCount := 0

	// Content width for boxes (viewport width minus border/padding)
	contentWidth := m.width - 10

	// Reset image/audio references - we'll collect them as we render
	m.imageRefs = nil
	m.audioRefs = nil
	imageCounter := 0
	audioCounter := 0

	// Request metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %s\n%s %.1f\n%s %d\n%s %v",
		labelStyle.Render("Model:"), req.Model,
		labelStyle.Render("Endpoint:"), m.selected.Path,
		labelStyle.Render("Input Tokens:"), formatMessagesInputTokenCount(m.selected),
		labelStyle.Render("Temperature:"), req.Temperature,
		labelStyle.Render("Max Tokens:"), req.MaxTokens,
		labelStyle.Render("Stream:"), req.Stream,
	)
	metaRendered := metaBox.Render(meta)
	b.WriteString(metaRendered)
	b.WriteString("\n\n")
	lineCount += strings.Count(metaRendered, "\n") + 2

	// Tool definitions (if present in request)
	tools := extractToolDefinitions(m.selected.RequestBody)
	if len(tools) > 0 {
		toolsRendered, toolsLines := m.renderToolDefinitions(tools, contentWidth, contentWidth-6)
		b.WriteString(toolsRendered)
		lineCount += toolsLines
	}

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
						} else if t == "input_audio" {
							// Extract audio data and create a clickable reference
							audioCounter++
							if data, format, ok := extractInputAudioData(p); ok {
								m.audioRefs = append(m.audioRefs, AudioRef{
									Index:  audioCounter,
									Data:   data,
									Format: format,
								})
							}
							parts = append(parts, fmt.Sprintf("{AUDIO_PLACEHOLDER_%d}", audioCounter))
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
			// Replace image/audio placeholders with simple text for preview
			for _, img := range m.imageRefs {
				placeholder := fmt.Sprintf("{IMG_PLACEHOLDER_%d}", img.Index)
				preview = strings.Replace(preview, placeholder, fmt.Sprintf("[Image %d]", img.Index), 1)
			}
			for _, audio := range m.audioRefs {
				placeholder := fmt.Sprintf("{AUDIO_PLACEHOLDER_%d}", audio.Index)
				preview = strings.Replace(preview, placeholder, fmt.Sprintf("[Audio %d]", audio.Index), 1)
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

			renderedContent := m.replaceAudioPlaceholders(m.replaceImagePlaceholders(renderContentSmart(content, textWidth)))

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
				reasoningBlock = reasoningInnerBox.Render(reasoningLabel + "\n\n" + renderMarkdown(reasoningText, textWidth-6))
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

// replaceAudioPlaceholders replaces {AUDIO_PLACEHOLDER_N} with styled clickable audio links
func (m *model) replaceAudioPlaceholders(content string) string {
	for _, audio := range m.audioRefs {
		placeholder := fmt.Sprintf("{AUDIO_PLACEHOLDER_%d}", audio.Index)
		audioZoneID := fmt.Sprintf("audio-%d", audio.Index)
		audioStyle := lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true).
			Underline(true)
		label := fmt.Sprintf("[Audio %d - %s]", audio.Index, audio.Format)
		if audio.Transcript != "" {
			label = fmt.Sprintf("[Audio %d - %s] %s", audio.Index, audio.Format, audio.Transcript)
		}
		styledPlaceholder := zone.Mark(audioZoneID, audioStyle.Render(label))
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

	if isGeminiEndpoint(m.selected.Path) {
		return m.renderGeminiOutputTab()
	}

	var resp OpenAIResponse
	if err := json.Unmarshal(m.selected.ResponseBody, &resp); err != nil {
		// Try reassembling SSE streaming chunks into a structured response
		if isSSEData(m.selected.ResponseBody) {
			if assembled := reassembleSSEResponse(m.selected.ResponseBody); assembled != nil {
				// Use extracted token counts from the request if SSE usage is missing
				if assembled.Usage.PromptTokens == 0 && m.selected.InputTokens > 0 {
					assembled.Usage.PromptTokens = m.selected.InputTokens
				}
				if assembled.Usage.CompletionTokens == 0 && m.selected.OutputTokens > 0 {
					assembled.Usage.CompletionTokens = m.selected.OutputTokens
				}
				if assembled.Usage.TotalTokens == 0 {
					assembled.Usage.TotalTokens = assembled.Usage.PromptTokens + assembled.Usage.CompletionTokens
				}
				resp = *assembled
			} else {
				return renderJSONBody(m.selected.ResponseBody, "Response")
			}
		} else {
			return renderJSONBody(m.selected.ResponseBody, "Response")
		}
	}

	// Try to extract audio output format from request body
	audioOutputFormat := "wav"
	if len(m.selected.RequestBody) > 0 {
		var reqBody struct {
			Audio *struct {
				Format string `json:"format"`
			} `json:"audio"`
		}
		if json.Unmarshal(m.selected.RequestBody, &reqBody) == nil && reqBody.Audio != nil && reqBody.Audio.Format != "" {
			audioOutputFormat = reqBody.Audio.Format
		}
	}
	// Set format on response audio objects that don't have it
	for i := range resp.Choices {
		if resp.Choices[i].Message.Audio != nil && resp.Choices[i].Message.Audio.Format == "" {
			resp.Choices[i].Message.Audio.Format = audioOutputFormat
		}
	}

	var b strings.Builder
	lineCount := 0
	audioCounter := 0
	m.audioRefs = nil

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
	if m.selected.IsStreaming {
		modeLabel := "Streaming (reassembled)"
		if m.selected.Status == StatusPending {
			modeLabel = "Streaming live…"
		}
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("Mode:"), modeLabel)
	}
	if m.selected.TTFT > 0 {
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("TTFT:"), formatDuration(m.selected.TTFT))
	}
	if m.selected.Duration > 0 {
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("Total Latency:"), formatDuration(m.selected.Duration))
	}
	if m.selected.Cost > 0 {
		meta += fmt.Sprintf("\n%s %s", labelStyle.Render("Cost:"), formatCost(m.selected.Cost))
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

		// Build reasoning block if present
		reasoningBlock := ""
		if choice.Message.ReasoningContent != "" {
			reasoningLabel := lipgloss.NewStyle().
				Foreground(dimColor).
				Bold(true).
				Render("💭 THINKING")
			reasoningText := sanitizeForTerminal(choice.Message.ReasoningContent)
			reasoningInnerBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(dimColor).
				Padding(0, 2).
				Width(textWidth)
			reasoningBlock = reasoningInnerBox.Render(reasoningLabel+"\n\n"+renderMarkdown(reasoningText, textWidth-6)) + "\n\n"
		}

		// Build audio block if present
		audioBlock := ""
		if choice.Message.Audio != nil && choice.Message.Audio.Data != "" {
			audioCounter++
			audioFormat := choice.Message.Audio.Format
			if audioFormat == "" {
				audioFormat = "wav"
			}
			m.audioRefs = append(m.audioRefs, AudioRef{
				Index:      audioCounter,
				Data:       choice.Message.Audio.Data,
				Format:     audioFormat,
				Transcript: choice.Message.Audio.Transcript,
				IsOutput:   true,
			})
			audioZoneID := fmt.Sprintf("audio-%d", audioCounter)
			audioStyle := lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				Underline(true)
			audioLink := zone.Mark(audioZoneID, audioStyle.Render(fmt.Sprintf("[Play Audio %d - %s]", audioCounter, audioFormat)))
			transcriptText := ""
			if choice.Message.Audio.Transcript != "" {
				transcriptText = "\n\n" + renderMarkdown(sanitizeForTerminal(choice.Message.Audio.Transcript), textWidth)
			}
			audioInnerBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accentColor).
				Padding(0, 2).
				Width(textWidth)
			audioBlock = audioInnerBox.Render("🔊 AUDIO OUTPUT\n\n"+audioLink+transcriptText) + "\n\n"
		}

		var msgContent string
		hasToolCalls := len(choice.Message.ToolCalls) > 0

		if hasToolCalls {
			// Tool call response
			msgContent = roleStyled + "\n\n"
			msgContent += reasoningBlock + audioBlock
			if content != "" && content != "null" && content != "\"\"" {
				msgContent += renderMarkdown(content, textWidth) + "\n\n"
			}
			msgContent += m.renderToolCalls(choice.Message.ToolCalls, textWidth)
			msgContent += "\n\n" + finishInfo
		} else {
			// Regular text response
			msgContent = roleStyled + "\n\n" + reasoningBlock + audioBlock + renderMarkdown(content, textWidth) + "\n\n" + finishInfo
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
	if m.selected.Status == StatusPending && len(m.selected.ResponseBody) == 0 {
		return pendingStyle.Render("⏳ Waiting for response...")
	}

	var b strings.Builder

	// HTTP Status Line
	statusCode := m.selected.StatusCode
	statusText := http.StatusText(statusCode)
	statusColor := successColor
	if statusCode == 0 && m.selected.Status == StatusPending {
		statusColor = warningColor
		statusText = "Streaming in progress"
	}
	if statusCode >= 400 {
		statusColor = errorColor
	} else if statusCode >= 300 {
		statusColor = warningColor
	}

	statusLineCode := fmt.Sprintf("HTTP/1.1 %d", statusCode)
	if statusCode == 0 && m.selected.Status == StatusPending {
		statusLineCode = "HTTP/1.1 …"
	}
	statusLine := fmt.Sprintf("%s %s",
		lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusLineCode),
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
	m.audioRefs = nil

	// Request metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %s\n%s %d\n%s %v",
		labelStyle.Render("Model:"), req.Model,
		labelStyle.Render("Endpoint:"), m.selected.Path,
		labelStyle.Render("Input Tokens:"), formatMessagesInputTokenCount(m.selected),
		labelStyle.Render("Max Tokens:"), req.MaxTokens,
		labelStyle.Render("Stream:"), req.Stream,
	)
	metaRendered := metaBox.Render(meta)
	b.WriteString(metaRendered)
	b.WriteString("\n\n")
	lineCount += strings.Count(metaRendered, "\n") + 2

	// Tool definitions (if present in request)
	tools := extractToolDefinitions(m.selected.RequestBody)
	if len(tools) > 0 {
		toolsRendered, toolsLines := m.renderToolDefinitions(tools, contentWidth, textWidth)
		b.WriteString(toolsRendered)
		lineCount += toolsLines
	}

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
	audioCounter := 0

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
				case "input_audio":
					audioCounter++
					if data, format, ok := extractInputAudioData(bl); ok {
						m.audioRefs = append(m.audioRefs, AudioRef{
							Index:  audioCounter,
							Data:   data,
							Format: format,
						})
					}
					parts = append(parts, fmt.Sprintf("{AUDIO_PLACEHOLDER_%d}", audioCounter))
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
			// Replace image/audio placeholders with simple text for preview
			for _, img := range m.imageRefs {
				placeholder := fmt.Sprintf("{IMG_PLACEHOLDER_%d}", img.Index)
				preview = strings.Replace(preview, placeholder, fmt.Sprintf("[Image %d]", img.Index), 1)
			}
			for _, audio := range m.audioRefs {
				placeholder := fmt.Sprintf("{AUDIO_PLACEHOLDER_%d}", audio.Index)
				preview = strings.Replace(preview, placeholder, fmt.Sprintf("[Audio %d]", audio.Index), 1)
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

			renderedContent := m.replaceAudioPlaceholders(m.replaceImagePlaceholders(renderContentSmart(content, textWidth)))

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

func (m *model) renderGeminiMessagesTab() string {
	var req struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		Model             string  `json:"model"`
		GenerationConfig  struct {
			Temperature float64 `json:"temperature"`
			MaxTokens   int     `json:"maxOutputTokens"`
		} `json:"generationConfig"`
		SystemInstruction *struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
	}

	if err := json.Unmarshal(m.selected.RequestBody, &req); err != nil {
		return errorStyle.Render(fmt.Sprintf("Failed to parse request: %v", err))
	}

	var b strings.Builder
	contentWidth := m.width - 10
	textWidth := contentWidth - 6

	// Request metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("%s %s\n%s %s\n%s %s\n%s %.1f\n%s %d\n%s %v",
		labelStyle.Render("Model:"), req.Model,
		labelStyle.Render("Endpoint:"), m.selected.Path,
		labelStyle.Render("Input Tokens:"), formatMessagesInputTokenCount(m.selected),
		labelStyle.Render("Temperature:"), req.GenerationConfig.Temperature,
		labelStyle.Render("Max Tokens:"), req.GenerationConfig.MaxTokens,
		labelStyle.Render("Stream:"), m.selected.IsStreaming,
	)
	b.WriteString(metaBox.Render(meta))
	b.WriteString("\n\n")

	// System instruction
	if req.SystemInstruction != nil {
		for _, p := range req.SystemInstruction.Parts {
			if p.Text != "" {
				sysBox := lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("5")).
					Padding(0, 2).
					MarginBottom(1).
					MaxWidth(contentWidth)
				header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render("SYSTEM")
				b.WriteString(sysBox.Render(header + "\n\n" + wrapText(p.Text, textWidth)))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString(labelStyle.Render("═══ Messages ═══"))
	b.WriteString("\n\n")

	// Render contents (messages)
	for _, msg := range req.Contents {
		role := strings.ToUpper(msg.Role)
		var roleColor lipgloss.Color
		var roleIcon string
		if msg.Role == "user" {
			roleColor = lipgloss.Color("3")
			roleIcon = "👤"
		} else {
			roleColor = lipgloss.Color("2")
			roleIcon = "🤖"
		}

		msgBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(roleColor).
			Padding(0, 2).
			MarginBottom(1).
			MaxWidth(contentWidth)

		header := lipgloss.NewStyle().Bold(true).Foreground(roleColor).Render(roleIcon + " " + role)
		var content string
		for _, p := range msg.Parts {
			if p.Text != "" {
				content += wrapText(p.Text, textWidth)
			}
		}
		if content != "" {
			b.WriteString(msgBox.Render(header + "\n\n" + content))
		} else {
			b.WriteString(msgBox.Render(header))
		}
		b.WriteString("\n")
	}

	// Response
	if len(m.selected.ResponseBody) > 0 {
		var geminiResp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text    string `json:"text"`
						Thought bool   `json:"thought"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}

		effectiveBody := m.selected.ResponseBody
		if isSSEData(m.selected.ResponseBody) {
			if decoded := reassembleGeminiSSE(m.selected.ResponseBody); decoded != nil {
				effectiveBody = decoded
			}
		}

		if json.Unmarshal(effectiveBody, &geminiResp) == nil {
			for _, c := range geminiResp.Candidates {
				for _, p := range c.Content.Parts {
					if p.Text == "" {
						continue
					}
					roleColor := lipgloss.Color("2")
					label := "🤖 MODEL"
					if p.Thought {
						roleColor = lipgloss.Color("13")
						label = "💭 THINKING"
					}
					msgBox := lipgloss.NewStyle().
						Border(lipgloss.RoundedBorder()).
						BorderForeground(roleColor).
						Padding(0, 2).
						MarginBottom(1).
						MaxWidth(contentWidth)
					header := lipgloss.NewStyle().Bold(true).Foreground(roleColor).Render(label)
					b.WriteString(msgBox.Render(header + "\n\n" + wrapText(p.Text, textWidth)))
					b.WriteString("\n")
				}
			}
		}
	}

	return b.String()
}

func (m *model) renderGeminiOutputTab() string {
	if len(m.selected.ResponseBody) == 0 {
		if m.selected.Status == StatusPending {
			return pendingStyle.Render("⏳ Waiting for response...")
		}
		return contentStyle.Render("No response body")
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text             string `json:"text"`
					Thought          bool   `json:"thought"`
					ThoughtSignature string `json:"thoughtSignature"`
				} `json:"parts"`
				Role string `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
			ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
		ModelVersion string `json:"modelVersion"`
	}

	effectiveBody := m.selected.ResponseBody
	// Handle Gemini SSE streaming
	if isSSEData(m.selected.ResponseBody) {
		if decoded := reassembleGeminiSSE(m.selected.ResponseBody); decoded != nil {
			effectiveBody = decoded
		}
	}

	if err := json.Unmarshal(effectiveBody, &resp); err != nil {
		return renderJSONBody(m.selected.ResponseBody, "Response")
	}

	var b strings.Builder
	contentWidth := m.width - 10
	textWidth := contentWidth - 6

	// Response metadata
	metaBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(successColor).
		Padding(0, 2).
		MarginBottom(1).
		MaxWidth(contentWidth)

	meta := fmt.Sprintf("Model: %s\nPrompt Tokens: %d\nCandidate Tokens: %d\nTotal Tokens: %d",
		resp.ModelVersion,
		resp.UsageMetadata.PromptTokenCount,
		resp.UsageMetadata.CandidatesTokenCount,
		resp.UsageMetadata.TotalTokenCount,
	)
	if resp.UsageMetadata.ThoughtsTokenCount > 0 {
		meta += fmt.Sprintf("\nThoughts Tokens: %d", resp.UsageMetadata.ThoughtsTokenCount)
	}
	if m.selected.Cost > 0 {
		meta += fmt.Sprintf("\nCost: $%.4f", m.selected.Cost)
	}
	b.WriteString(metaBox.Render(meta))
	b.WriteString("\n\n")

	// Candidates
	for i, candidate := range resp.Candidates {
		b.WriteString(labelStyle.Render(fmt.Sprintf("═══ Candidate %d (finish: %s) ═══", i, candidate.FinishReason)))
		b.WriteString("\n\n")

		for _, part := range candidate.Content.Parts {
			if part.Thought {
				thinkBox := lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("13")).
					Padding(0, 2).
					MarginBottom(1).
					MaxWidth(contentWidth)
				b.WriteString(thinkBox.Render(wrapText(part.Text, textWidth)))
				b.WriteString("\n")
			} else if part.Text != "" {
				textBox := lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(successColor).
					Padding(0, 2).
					MarginBottom(1).
					MaxWidth(contentWidth)
				b.WriteString(textBox.Render(wrapText(part.Text, textWidth)))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// reassembleGeminiSSE reassembles Gemini SSE streaming chunks into a single JSON response.
func reassembleGeminiSSE(data []byte) []byte {
	lines := strings.Split(string(data), "\n")

	var lastChunk json.RawMessage
	var allText strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data:") {
			continue
		}
		jsonData := strings.TrimPrefix(line, "data: ")
		jsonData = strings.TrimPrefix(jsonData, "data:")
		jsonData = strings.TrimSpace(jsonData)
		if jsonData == "" || jsonData == "[DONE]" {
			continue
		}

		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if json.Unmarshal([]byte(jsonData), &chunk) == nil {
			for _, c := range chunk.Candidates {
				for _, p := range c.Content.Parts {
					allText.WriteString(p.Text)
				}
			}
		}
		lastChunk = json.RawMessage(jsonData)
	}

	if lastChunk == nil {
		return nil
	}

	// Use the last chunk (which has final usageMetadata) but replace the text with accumulated text
	var final map[string]interface{}
	if json.Unmarshal(lastChunk, &final) != nil {
		return nil
	}

	// Replace candidate text with accumulated text
	if candidates, ok := final["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			if content, ok := candidate["content"].(map[string]interface{}); ok {
				content["parts"] = []map[string]interface{}{{"text": allText.String()}}
			}
		}
	}

	result, err := json.Marshal(final)
	if err != nil {
		return nil
	}
	return result
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
		// Try reassembling Anthropic SSE or Bedrock binary streaming events
		if assembled := reassembleAnthropicSSEResponse(m.selected.ResponseBody); assembled != nil {
			if assembled.Usage.InputTokens == 0 && m.selected.InputTokens > 0 {
				assembled.Usage.InputTokens = m.selected.InputTokens
			}
			if assembled.Usage.OutputTokens == 0 && m.selected.OutputTokens > 0 {
				assembled.Usage.OutputTokens = m.selected.OutputTokens
			}
			resp = *assembled
		} else {
			return renderJSONBody(m.selected.ResponseBody, "Response")
		}
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

// extractToolDefinitions extracts tool definitions from a request body JSON.
// Returns the raw tool maps, or nil if no tools found.
func extractToolDefinitions(body []byte) []map[string]interface{} {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	tools, ok := raw["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		return nil
	}
	var result []map[string]interface{}
	for _, t := range tools {
		if tool, ok := t.(map[string]interface{}); ok {
			result = append(result, tool)
		}
	}
	return result
}

// getToolNameAndDesc extracts name and description from a tool definition map,
// handling both OpenAI format (function.name) and Anthropic format (name).
func getToolNameAndDesc(tool map[string]interface{}) (name, description string, schema interface{}) {
	// OpenAI format: tools[].function.{name, description, parameters}
	if fn, ok := tool["function"].(map[string]interface{}); ok {
		name, _ = fn["name"].(string)
		description, _ = fn["description"].(string)
		schema = fn["parameters"]
	}
	// Anthropic format: tools[].{name, description, input_schema}
	if name == "" {
		name, _ = tool["name"].(string)
		description, _ = tool["description"].(string)
		schema = tool["input_schema"]
	}
	return
}

// renderToolDefinitions renders a collapsible tools section showing tool definitions.
// Returns the rendered string and the number of lines it occupies.
func (m *model) renderToolDefinitions(tools []map[string]interface{}, contentWidth, textWidth int) (string, int) {
	if len(tools) == 0 {
		return "", 0
	}

	var b strings.Builder
	lineCount := 0

	// Collect tool names for collapsed preview
	var toolNames []string
	for _, tool := range tools {
		name, _, _ := getToolNameAndDesc(tool)
		if name != "" {
			toolNames = append(toolNames, name)
		}
	}

	// Header
	collapseIcon := "▼"
	if m.toolsCollapsed {
		collapseIcon = "▶"
	}
	collapseIndicator := lipgloss.NewStyle().Foreground(dimColor).Render(collapseIcon)

	toolHeader := lipgloss.NewStyle().
		Foreground(warningColor).
		Bold(true).
		Render(fmt.Sprintf("🛠 TOOLS (%d)", len(tools)))

	clickableHeader := zone.Mark("tools-def", collapseIndicator+" "+toolHeader)

	toolBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(warningColor).
		Padding(0, 2).
		Width(contentWidth)

	if m.toolsCollapsed {
		// Collapsed: show header with tool names preview
		preview := strings.Join(toolNames, ", ")
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		previewStyled := lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true).
			Render(preview)
		content := fmt.Sprintf("%s  %s", clickableHeader, previewStyled)
		rendered := toolBox.Render(content)
		b.WriteString(rendered)
		b.WriteString("\n\n")
		lineCount += strings.Count(rendered, "\n") + 2
	} else {
		// Expanded: show each tool with description and schema
		var content strings.Builder
		content.WriteString(clickableHeader)
		content.WriteString("\n")

		for i, tool := range tools {
			name, desc, schema := getToolNameAndDesc(tool)

			nameStyled := lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true).
				Render(fmt.Sprintf("▸ %s", name))
			content.WriteString("\n")
			content.WriteString(nameStyled)
			content.WriteString("\n")

			if desc != "" {
				// Truncate long descriptions
				if len(desc) > 200 {
					desc = desc[:197] + "..."
				}
				descStyled := lipgloss.NewStyle().
					Foreground(dimColor).
					Italic(true).
					Width(textWidth).
					Render(desc)
				content.WriteString(descStyled)
				content.WriteString("\n")
			}

			if schema != nil {
				schemaJSON, err := json.MarshalIndent(schema, "    ", "  ")
				if err == nil {
					schemaLabel := lipgloss.NewStyle().
						Foreground(accentColor).
						Render("  Parameters:")
					content.WriteString(schemaLabel)
					content.WriteString("\n    ")
					content.WriteString(highlightJSON(string(schemaJSON)))
					content.WriteString("\n")
				}
			}

			if i < len(tools)-1 {
				content.WriteString(lipgloss.NewStyle().Foreground(borderColor).Render("  ─────────────────────────"))
				content.WriteString("\n")
			}
		}

		rendered := toolBox.Render(content.String())
		b.WriteString(rendered)
		b.WriteString("\n\n")
		lineCount += strings.Count(rendered, "\n") + 2
	}

	return b.String(), lineCount
}

// truncateID shortens long tool call IDs for display
func truncateID(id string) string {
	if len(id) <= 20 {
		return id
	}
	return id[:8] + "..." + id[len(id)-8:]
}
