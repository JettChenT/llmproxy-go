package main

import (
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Messages for TUI updates
type requestAddedMsg struct{ req *LLMRequest }
type requestUpdatedMsg struct{ req *LLMRequest }
type tickMsg time.Time

// TUI Model
type model struct {
	requests     []*LLMRequest
	cursor       int
	selectedID   int // Track selected request by ID instead of index
	selected     *LLMRequest
	width        int
	height       int
	viewport     viewport.Model
	activeTab    Tab
	showDetail   bool
	ready        bool
	listenAddr   string
	targetURL    string
	followLatest bool // Auto-scroll to latest request

	// Vim-style navigation
	numBuffer     string // Accumulates digits for count prefix (e.g., "10" in "10j")
	commandMode   bool   // True when entering a : command
	commandBuffer string // Accumulates the command after :
}

func initialModel(listenAddr, targetURL string) model {
	return model{
		requests:     make([]*LLMRequest, 0),
		listenAddr:   listenAddr,
		targetURL:    targetURL,
		followLatest: false, // Start with follow mode off
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), tea.EnterAltScreen, tea.EnableMouseCellMotion)
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()

		// Handle command mode (after pressing :)
		if m.commandMode {
			switch key {
			case "esc":
				m.commandMode = false
				m.commandBuffer = ""
			case "enter":
				// Execute the command - jump to request ID
				if m.commandBuffer != "" {
					if targetID, err := parseNumber(m.commandBuffer); err == nil && targetID > 0 {
						// Find request with matching ID
						for i, req := range m.requests {
							if req.ID == targetID {
								m.cursor = i
								m.followLatest = false
								break
							}
						}
					}
				}
				m.commandMode = false
				m.commandBuffer = ""
			case "backspace":
				if len(m.commandBuffer) > 0 {
					m.commandBuffer = m.commandBuffer[:len(m.commandBuffer)-1]
				}
			default:
				// Only accept digits for the goto command
				if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
					m.commandBuffer += key
				}
			}
			return m, nil
		}

		// Handle number prefix for vim-style navigation (e.g., 10j, 5k)
		if !m.showDetail && len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			// Don't allow leading zeros unless it's the only digit
			if key != "0" || m.numBuffer != "" {
				m.numBuffer += key
			}
			return m, nil
		}

		// Get the count from numBuffer (default to 1)
		count := 1
		if m.numBuffer != "" {
			if n, err := parseNumber(m.numBuffer); err == nil && n > 0 {
				count = n
			}
			m.numBuffer = "" // Clear buffer after use
		}

		switch key {
		case "ctrl+c", "q":
			if m.showDetail {
				m.showDetail = false
				m.selected = nil
				return m, nil
			}
			return m, tea.Quit

		case ":":
			// Enter command mode for :N to jump to request ID
			if !m.showDetail {
				m.commandMode = true
				m.commandBuffer = ""
			}

		case "up", "k":
			if !m.showDetail && m.cursor > 0 {
				m.cursor -= count
				if m.cursor < 0 {
					m.cursor = 0
				}
				m.followLatest = false // Disable follow mode when manually navigating
			}

		case "down", "j":
			if !m.showDetail && m.cursor < len(m.requests)-1 {
				m.cursor += count
				if m.cursor >= len(m.requests) {
					m.cursor = len(m.requests) - 1
				}
				m.followLatest = false // Disable follow mode when manually navigating
			}

		case "g", "home":
			// Jump to oldest (top)
			if !m.showDetail {
				m.cursor = 0
				m.followLatest = false
			}

		case "G", "end":
			// Jump to newest (bottom)
			if !m.showDetail && len(m.requests) > 0 {
				m.cursor = len(m.requests) - 1
				m.followLatest = false
			}

		case "f":
			// Toggle follow mode (auto-scroll to newest at bottom)
			if !m.showDetail {
				m.followLatest = !m.followLatest
				if m.followLatest && len(m.requests) > 0 {
					m.cursor = len(m.requests) - 1
				}
			}

		case "enter":
			if !m.showDetail && len(m.requests) > 0 && m.cursor < len(m.requests) {
				m.showDetail = true
				m.selected = m.requests[m.cursor]
				m.selectedID = m.selected.ID
				m.activeTab = TabMessages
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}

		case "esc":
			if m.showDetail {
				m.showDetail = false
				m.selected = nil
			}
			m.numBuffer = "" // Clear any pending number

		case "tab", "l":
			if m.showDetail {
				m.activeTab = (m.activeTab + 1) % 4
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}

		case "shift+tab", "h":
			if m.showDetail {
				m.activeTab = (m.activeTab + 3) % 4
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}

		case "1":
			if m.showDetail {
				m.activeTab = TabMessages
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}

		case "2":
			if m.showDetail {
				m.activeTab = TabOutput
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}

		case "3":
			if m.showDetail {
				m.activeTab = TabRawInput
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}

		case "4":
			if m.showDetail {
				m.activeTab = TabRawOutput
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}
		}

	case tea.MouseMsg:
		// Handle mouse clicks in list view
		if !m.showDetail && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			// Calculate which row was clicked
			// Header takes 4 lines (title + blank + column headers + separator)
			headerLines := 4
			clickedRow := msg.Y - headerLines
			listHeight := m.height - 8

			if clickedRow >= 0 && clickedRow < listHeight {
				// Calculate the actual index based on scroll position
				start := 0
				if len(m.requests) > listHeight && m.cursor >= listHeight {
					start = m.cursor - listHeight + 1
				}

				actualIndex := start + clickedRow
				if actualIndex >= 0 && actualIndex < len(m.requests) {
					m.cursor = actualIndex
					m.followLatest = false

					// Single click shows detail for better UX
					m.showDetail = true
					m.selected = m.requests[m.cursor]
					m.selectedID = m.selected.ID
					m.activeTab = TabMessages
					m.viewport.SetContent(m.renderTabContent())
					m.viewport.GotoTop()
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 4
		footerHeight := 2
		tabHeight := 3
		viewportHeight := m.height - headerHeight - footerHeight - tabHeight - 4

		if !m.ready {
			m.viewport = viewport.New(m.width-4, viewportHeight)
			m.viewport.Style = viewportStyle
			m.ready = true
		} else {
			m.viewport.Width = m.width - 4
			m.viewport.Height = viewportHeight
		}

	case requestAddedMsg:
		// Append new requests to the end (chronological order: oldest at top, newest at bottom)
		m.requests = append(m.requests, msg.req)

		// In follow mode, jump to the newest (last) item
		if m.followLatest {
			m.cursor = len(m.requests) - 1
		}

	case requestUpdatedMsg:
		// Request already updated in place
		if m.showDetail && m.selected != nil && m.selected.ID == msg.req.ID {
			m.viewport.SetContent(m.renderTabContent())
		}

	case tickMsg:
		// Refresh the view from global state
		requestsMu.RLock()
		newRequests := make([]*LLMRequest, len(requests))
		copy(newRequests, requests)
		requestsMu.RUnlock()

		// Update requests list
		m.requests = newRequests

		// Clamp cursor to valid range
		if m.cursor >= len(m.requests) {
			m.cursor = max(0, len(m.requests)-1)
		}

		cmds = append(cmds, tickCmd())
	}

	if m.showDetail {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.showDetail {
		return m.renderDetailView()
	}

	return m.renderListView()
}
