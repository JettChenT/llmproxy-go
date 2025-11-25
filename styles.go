package main

import "github.com/charmbracelet/lipgloss"

// Color palette - cyberpunk inspired
var (
	primaryColor   = lipgloss.Color("#00D9FF")
	secondaryColor = lipgloss.Color("#FF6B6B")
	accentColor    = lipgloss.Color("#A855F7")
	successColor   = lipgloss.Color("#4ADE80")
	warningColor   = lipgloss.Color("#FBBF24")
	errorColor     = lipgloss.Color("#F87171")
	dimColor       = lipgloss.Color("#6B7280")
	bgColor        = lipgloss.Color("#0F172A")
	surfaceColor   = lipgloss.Color("#1E293B")
	borderColor    = lipgloss.Color("#334155")
)

// Styles
var (
	// Title style
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Background(surfaceColor).
			Padding(0, 2).
			MarginBottom(1)

	// Status bar style
	statusBarStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Background(surfaceColor).
			Padding(0, 1)

	// List item styles
	itemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E2E8F0")).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Background(surfaceColor).
				Bold(true).
				Padding(0, 1)

	// Status styles
	pendingStyle = lipgloss.NewStyle().
			Foreground(warningColor).
			Bold(true)

	completeStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	// Tab styles
	activeTabStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Background(surfaceColor).
			Bold(true).
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(dimColor).
				Background(bgColor).
				Padding(0, 2).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderColor)

	// Content styles
	labelStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	contentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E2E8F0"))

	// Help style
	helpStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Padding(0, 1)

	// Model badge style
	modelBadgeStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	// Viewport border
	viewportStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)
)

