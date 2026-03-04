package main

import "github.com/charmbracelet/lipgloss"

// Color palette - resolved from currentTheme at init time
var (
	primaryColor   lipgloss.Color
	secondaryColor lipgloss.Color
	accentColor    lipgloss.Color
	successColor   lipgloss.Color
	warningColor   lipgloss.Color
	errorColor     lipgloss.Color
	dimColor       lipgloss.Color
	bgColor        lipgloss.Color
	surfaceColor   lipgloss.Color
	borderColor    lipgloss.Color
	textColor      lipgloss.Color
)

// Styles - rebuilt after theme is selected
var (
	titleStyle       lipgloss.Style
	statusBarStyle   lipgloss.Style
	itemStyle        lipgloss.Style
	selectedItemStyle lipgloss.Style
	pendingStyle     lipgloss.Style
	completeStyle    lipgloss.Style
	errorStyle       lipgloss.Style
	activeTabStyle   lipgloss.Style
	inactiveTabStyle lipgloss.Style
	labelStyle       lipgloss.Style
	contentStyle     lipgloss.Style
	helpStyle        lipgloss.Style
	modelBadgeStyle  lipgloss.Style
	viewportStyle    lipgloss.Style
)

// initStyles rebuilds all styles from the current theme. Must be called after
// initTheme() so that color variables are set.
func initStyles() {
	primaryColor = currentTheme.Primary
	secondaryColor = currentTheme.Secondary
	accentColor = currentTheme.Accent
	successColor = currentTheme.Success
	warningColor = currentTheme.Warning
	errorColor = currentTheme.Error
	dimColor = currentTheme.Dim
	bgColor = currentTheme.Bg
	surfaceColor = currentTheme.Surface
	borderColor = currentTheme.Border
	textColor = currentTheme.Text

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Background(surfaceColor).
		Padding(0, 2).
		MarginBottom(1)

	statusBarStyle = lipgloss.NewStyle().
		Foreground(dimColor).
		Background(surfaceColor).
		Padding(0, 1)

	itemStyle = lipgloss.NewStyle().
		Foreground(textColor).
		Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
		Foreground(primaryColor).
		Background(surfaceColor).
		Bold(true).
		Padding(0, 1)

	pendingStyle = lipgloss.NewStyle().
		Foreground(warningColor).
		Bold(true)

	completeStyle = lipgloss.NewStyle().
		Foreground(successColor).
		Bold(true)

	errorStyle = lipgloss.NewStyle().
		Foreground(errorColor).
		Bold(true)

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

	labelStyle = lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true)

	contentStyle = lipgloss.NewStyle().
		Foreground(textColor)

	helpStyle = lipgloss.NewStyle().
		Foreground(dimColor).
		Padding(0, 1)

	modelBadgeStyle = lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true)

	viewportStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)
}
