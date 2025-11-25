package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Cached markdown renderers by width
var markdownRenderers = make(map[int]*glamour.TermRenderer)

// Cached base style to avoid expensive detection on every render
var baseStyle glamour.TermRendererOption

// InitMarkdownRenderer pre-warms the markdown renderer cache for the given width
func InitMarkdownRenderer(width int) {
	getMarkdownRenderer(width)
}

// getMarkdownRenderer returns a cached markdown renderer or creates one
func getMarkdownRenderer(width int) *glamour.TermRenderer {
	// Check cache first
	if r, ok := markdownRenderers[width]; ok {
		return r
	}

	// Initialize base style once
	if baseStyle == nil {
		if lipgloss.HasDarkBackground() {
			baseStyle = glamour.WithStandardStyle("dark")
		} else {
			baseStyle = glamour.WithStandardStyle("light")
		}
	}

	// Create a new renderer with the specified width
	r, err := glamour.NewTermRenderer(
		baseStyle,
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(lipgloss.ColorProfile()),
	)
	if err != nil {
		return nil
	}

	// Cache for reuse
	markdownRenderers[width] = r
	return r
}

// renderMarkdown renders markdown content for terminal display
func renderMarkdown(content string, width int) string {
	if content == "" {
		return ""
	}

	// Sanitize content first
	content = sanitizeForTerminal(content)

	renderer := getMarkdownRenderer(width)
	if renderer == nil {
		// Fall back to plain text wrapping if renderer fails
		return wrapText(content, width)
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		// Fall back to plain text wrapping on error
		return wrapText(content, width)
	}

	// Trim trailing whitespace/newlines that glamour might add
	return strings.TrimSpace(rendered)
}

// parseNumber parses a string into an integer
func parseNumber(s string) (int, error) {
	return strconv.Atoi(s)
}

// formatBytes formats bytes into human readable format
func formatBytes(b int) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := int64(b) / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatTokenCount formats token counts into human readable format (e.g., 1.2K, 15K)
func formatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 10000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	if tokens < 1000000 {
		return fmt.Sprintf("%.0fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
}

// formatCost formats cost in USD with appropriate precision
func formatCost(cost float64) string {
	if cost < 0.0001 {
		return "<$0.0001"
	}
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	if cost < 1 {
		return fmt.Sprintf("$%.3f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// wrapText wraps text to fit within a given width
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}

	var result strings.Builder
	lines := strings.Split(s, "\n")

	for _, line := range lines {
		if len(line) <= width {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		words := strings.Fields(line)
		currentLine := ""
		for _, word := range words {
			if len(currentLine)+len(word)+1 <= width {
				if currentLine != "" {
					currentLine += " "
				}
				currentLine += word
			} else {
				if currentLine != "" {
					result.WriteString(currentLine)
					result.WriteString("\n")
				}
				currentLine = word
			}
		}
		if currentLine != "" {
			result.WriteString(currentLine)
			result.WriteString("\n")
		}
	}

	return strings.TrimSuffix(result.String(), "\n")
}

// sanitizeForTerminal removes or replaces control characters that could mess up the terminal
func sanitizeForTerminal(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	for _, r := range s {
		switch {
		// Allow common whitespace
		case r == '\n' || r == '\t' || r == ' ':
			result.WriteRune(r)
		// Allow printable characters
		case unicode.IsPrint(r):
			result.WriteRune(r)
		// Replace carriage return with newline
		case r == '\r':
			// Skip \r, often followed by \n anyway
			continue
		// Replace other control characters with a placeholder
		case unicode.IsControl(r):
			result.WriteString("�")
		// Allow other unicode characters (emojis, etc)
		default:
			result.WriteRune(r)
		}
	}

	return result.String()
}

// highlightJSON applies syntax highlighting to JSON string
func highlightJSON(s string) string {
	// First sanitize the input
	s = sanitizeForTerminal(s)

	var result strings.Builder
	inString := false
	isKey := false
	escaped := false

	keyStyle := lipgloss.NewStyle().Foreground(primaryColor)
	stringStyle := lipgloss.NewStyle().Foreground(successColor)
	numberStyle := lipgloss.NewStyle().Foreground(warningColor)
	boolStyle := lipgloss.NewStyle().Foreground(accentColor)
	punctStyle := lipgloss.NewStyle().Foreground(dimColor)

	i := 0
	for i < len(s) {
		c := s[i]

		// Handle escape sequences
		if escaped {
			if isKey {
				result.WriteString(keyStyle.Render(string(c)))
			} else {
				result.WriteString(stringStyle.Render(string(c)))
			}
			escaped = false
			i++
			continue
		}

		if c == '\\' && inString {
			escaped = true
			if isKey {
				result.WriteString(keyStyle.Render(string(c)))
			} else {
				result.WriteString(stringStyle.Render(string(c)))
			}
			i++
			continue
		}

		if c == '"' {
			if !inString {
				inString = true
				// Check if this is a key (followed by :)
				isKey = false
				for j := i + 1; j < len(s); j++ {
					if s[j] == '\\' {
						j++ // Skip escaped character
						continue
					}
					if s[j] == '"' {
						for k := j + 1; k < len(s); k++ {
							if s[k] == ':' {
								isKey = true
								break
							} else if s[k] != ' ' && s[k] != '\n' && s[k] != '\t' {
								break
							}
						}
						break
					}
				}
			} else {
				inString = false
			}

			if isKey {
				result.WriteString(keyStyle.Render(string(c)))
			} else {
				result.WriteString(stringStyle.Render(string(c)))
			}
		} else if inString {
			if isKey {
				result.WriteString(keyStyle.Render(string(c)))
			} else {
				result.WriteString(stringStyle.Render(string(c)))
			}
		} else if c == '{' || c == '}' || c == '[' || c == ']' || c == ':' || c == ',' {
			result.WriteString(punctStyle.Render(string(c)))
		} else if (c >= '0' && c <= '9') || c == '-' || c == '.' {
			result.WriteString(numberStyle.Render(string(c)))
		} else if i+4 <= len(s) && (s[i:i+4] == "true" || s[i:i+4] == "null") {
			result.WriteString(boolStyle.Render(s[i : i+4]))
			i += 3
		} else if i+5 <= len(s) && s[i:i+5] == "false" {
			result.WriteString(boolStyle.Render(s[i : i+5]))
			i += 4
		} else {
			result.WriteByte(c)
		}
		i++
	}

	return result.String()
}
