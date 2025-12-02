package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// ModelCostSummary holds aggregated cost data for a model
type ModelCostSummary struct {
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         float64
	RequestCount int
}

// TapeCostBreakdown holds the complete cost analysis
type TapeCostBreakdown struct {
	Models            map[string]*ModelCostSummary
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCost         float64
	TotalRequests     int
}

// AnalyzeTapeCosts processes a tape and returns cost breakdown
func AnalyzeTapeCosts(tape *Tape) *TapeCostBreakdown {
	breakdown := &TapeCostBreakdown{
		Models: make(map[string]*ModelCostSummary),
	}

	for _, req := range tape.Requests {
		// Skip incomplete requests
		if req.Status == StatusPending {
			continue
		}

		model := req.Model
		if model == "" {
			model = "(unknown)"
		}

		if _, exists := breakdown.Models[model]; !exists {
			breakdown.Models[model] = &ModelCostSummary{
				Model: model,
			}
		}

		summary := breakdown.Models[model]
		summary.InputTokens += req.InputTokens
		summary.OutputTokens += req.OutputTokens
		summary.Cost += req.Cost
		summary.RequestCount++

		breakdown.TotalInputTokens += req.InputTokens
		breakdown.TotalOutputTokens += req.OutputTokens
		breakdown.TotalCost += req.Cost
		breakdown.TotalRequests++
	}

	return breakdown
}

// formatWithCommas formats a number with thousand separators
func formatWithCommas(n int) string {
	if n < 0 {
		return "-" + formatWithCommas(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	// Recursively format with commas
	return formatWithCommas(n/1000) + fmt.Sprintf(",%03d", n%1000)
}

// PrintCostBreakdown prints a formatted cost breakdown table
func PrintCostBreakdown(breakdown *TapeCostBreakdown, tapePath string) {
	// Use adaptive colors that work with terminal themes
	headerColor := lipgloss.AdaptiveColor{Light: "#5c4d9a", Dark: "#a78bfa"}
	titleColor := lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#22d3ee"}
	successColor := lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"}
	textColor := lipgloss.AdaptiveColor{Light: "#334155", Dark: "#94a3b8"}
	borderColor := lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#475569"}

	// Title style
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(titleColor).
		MarginBottom(1)

	// Header style
	headerStyle := lipgloss.NewStyle().
		Foreground(headerColor).
		Bold(true).
		Align(lipgloss.Center).
		Padding(0, 1)

	// Cell style
	cellStyle := lipgloss.NewStyle().Padding(0, 1).Foreground(textColor)

	// Total row style
	totalStyle := lipgloss.NewStyle().
		Foreground(successColor).
		Bold(true).
		Padding(0, 1)

	// Sort models by cost (descending)
	var sortedModels []*ModelCostSummary
	for _, summary := range breakdown.Models {
		sortedModels = append(sortedModels, summary)
	}
	sort.Slice(sortedModels, func(i, j int) bool {
		return sortedModels[i].Cost > sortedModels[j].Cost
	})

	// Build table rows
	var rows [][]string
	for _, summary := range sortedModels {
		rows = append(rows, []string{
			summary.Model,
			fmt.Sprintf("%d", summary.RequestCount),
			formatWithCommas(summary.InputTokens),
			formatWithCommas(summary.OutputTokens),
			formatCost(summary.Cost),
		})
	}

	// Create the table
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(borderColor)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		}).
		Headers("MODEL", "REQS", "INPUT TOKENS", "OUTPUT TOKENS", "COST").
		Rows(rows...)

	// Print the output
	fmt.Println()
	fmt.Println(titleStyle.Render(fmt.Sprintf("📊 Cost Breakdown: %s", tapePath)))
	fmt.Println()
	fmt.Println(t)
	fmt.Println()

	// Print totals
	totalBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(successColor).
		Padding(0, 2).
		MarginTop(1)

	totalsContent := fmt.Sprintf(
		"%s %d   %s %s   %s %s   %s %s",
		totalStyle.Render("Total Requests:"),
		breakdown.TotalRequests,
		totalStyle.Render("Input:"),
		formatWithCommas(breakdown.TotalInputTokens),
		totalStyle.Render("Output:"),
		formatWithCommas(breakdown.TotalOutputTokens),
		totalStyle.Render("Total Cost:"),
		formatCost(breakdown.TotalCost),
	)

	fmt.Println(totalBox.Render(totalsContent))
	fmt.Println()
}

// RunCostCommand runs the cost breakdown command
func RunCostCommand(tapePath string) {
	tape, err := LoadTape(tapePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading tape: %v\n", err)
		os.Exit(1)
	}

	if len(tape.Requests) == 0 {
		fmt.Fprintf(os.Stderr, "No requests found in tape file\n")
		os.Exit(1)
	}

	breakdown := AnalyzeTapeCosts(tape)
	PrintCostBreakdown(breakdown, tapePath)
}
