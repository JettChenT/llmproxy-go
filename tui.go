package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Messages for TUI updates
type requestAddedMsg struct{ req *LLMRequest }
type requestUpdatedMsg struct{ req *LLMRequest }
type tickMsg time.Time
type tapeSavedMsg struct{ filename string }
type tapeSaveErrorMsg struct{ err error }
type tapePlayMsg struct{}
type tapeRealtimeTickMsg struct{}

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

	// Search and sort
	searchMode       bool           // True when in search input mode
	searchQuery      string         // Current search query
	filteredRequests []*LLMRequest  // Requests matching search query
	sortField        SortField      // Current sort field
	sortDirection    SortDirection  // Current sort direction
	searchIndexCache map[int]string // Cache of searchable text per request ID

	// Tape mode
	tape             *Tape     // Loaded tape for playback
	tapeMode         bool      // True when viewing a tape
	tapePlaying      bool      // True when auto-playing tape
	tapeSpeed        int       // Playback speed multiplier (1, 2, 4, 8)
	tapeRealtime     bool      // True for real-time playback, false for step-through
	tapePlayStart    time.Time // Wall clock time when playback started
	tapePlayStartPos time.Time // Tape position when playback started
	saveTapeFile     string    // File being recorded to (if any)

	// Save dialog
	showSaveDialog  bool
	saveInput       textinput.Model
	saveMessage     string
	saveMessageTime time.Time

	// Message navigation in detail view
	collapsedMessages map[int]bool // Track collapsed state per message index
	messagePositions  []int        // Line positions of each message in viewport
	currentMsgIndex   int          // Currently focused message index

	// Mouse mode
	mouseEnabled bool // True when mouse capture is enabled (default true)

	// Copy feedback
	copyMessage     string
	copyMessageTime time.Time

	// Image references for current request
	imageRefs []ImageRef
}

func newSaveInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "session.tape"
	ti.CharLimit = 100
	ti.Width = 40
	return ti
}

func initialModel(listenAddr, targetURL string, saveTapeFile string) model {
	return model{
		requests:          make([]*LLMRequest, 0),
		listenAddr:        listenAddr,
		targetURL:         targetURL,
		followLatest:      false,
		tapeSpeed:         1,
		saveTapeFile:      saveTapeFile,
		collapsedMessages: make(map[int]bool),
		sortField:         SortByID,
		sortDirection:     SortAsc,
		searchIndexCache:  make(map[int]string),
		saveInput:         newSaveInput(),
		mouseEnabled:      true,
	}
}

func initialTapeModel(tape *Tape) model {
	return model{
		requests:          tape.Requests,
		tape:              tape,
		tapeMode:          true,
		tapeRealtime:      true,
		followLatest:      false,
		tapeSpeed:         1,
		listenAddr:        tape.Session.ListenAddr,
		targetURL:         tape.Session.TargetURL,
		collapsedMessages: make(map[int]bool),
		sortField:         SortByID,
		sortDirection:     SortAsc,
		searchIndexCache:  make(map[int]string),
		saveInput:         newSaveInput(),
		mouseEnabled:      true,
	}
}

func (m model) Init() tea.Cmd {
	zone.NewGlobal()
	return tea.Batch(tickCmd(), tea.EnterAltScreen, tea.EnableMouseCellMotion)
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func tapePlayCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return tapePlayMsg{}
	})
}

func tapeRealtimeTickCmd() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(t time.Time) tea.Msg {
		return tapeRealtimeTickMsg{}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()

		// Handle save dialog mode
		if m.showSaveDialog {
			switch key {
			case "esc":
				m.showSaveDialog = false
				m.saveInput.Reset()
				m.saveInput.Blur()
				return m, nil
			case "enter":
				filename := m.saveInput.Value()
				if filename == "" {
					filename = m.saveInput.Placeholder
				}
				if filepath.Ext(filename) == "" {
					filename += ".tape"
				}
				go func() {
					err := SaveSessionToTape(filename, m.listenAddr, m.targetURL)
					if err != nil {
						program.Send(tapeSaveErrorMsg{err: err})
					} else {
						program.Send(tapeSavedMsg{filename: filename})
					}
				}()
				m.showSaveDialog = false
				m.saveInput.Reset()
				m.saveInput.Blur()
				return m, nil
			}
			// Let textinput handle all other keys
			var cmd tea.Cmd
			m.saveInput, cmd = m.saveInput.Update(msg)
			return m, cmd
		}

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
						displayRequests := m.getDisplayRequests()
						for i, req := range displayRequests {
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

		// Handle search mode (after pressing /)
		if m.searchMode {
			switch key {
			case "esc":
				m.searchMode = false
				// Don't clear search query - keep filter active
			case "enter":
				m.searchMode = false
				m.filterRequests()
				// Reset cursor if it's out of range
				displayRequests := m.getDisplayRequests()
				if m.cursor >= len(displayRequests) {
					m.cursor = max(0, len(displayRequests)-1)
				}
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.filterRequests()
				}
			default:
				if len(key) == 1 {
					m.searchQuery += key
					m.filterRequests()
				}
			}
			return m, nil
		}

		// Handle number prefix for vim-style navigation (e.g., 10j, 5k)
		// In tape mode, let '0' pass through for "jump to start" functionality
		if !m.showDetail && len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			// In tape mode, '0' with empty buffer means "jump to start"
			if m.tapeMode && key == "0" && m.numBuffer == "" {
				// Fall through to handle '0' in the switch below
			} else if key != "0" || m.numBuffer != "" {
				m.numBuffer += key
				return m, nil
			} else {
				// '0' with empty buffer in non-tape mode - ignore
				return m, nil
			}
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

		case "s", "S":
			// Save tape (only in live mode, not tape playback)
			if !m.showDetail && !m.tapeMode && len(m.requests) > 0 {
				m.showSaveDialog = true
				m.saveInput.SetValue(fmt.Sprintf("session-%s", time.Now().Format("20060102-150405")))
				m.saveInput.Focus()
				return m, m.saveInput.Cursor.BlinkCmd()
			}

		// Tape playback controls
		case " ":
			// Space toggles play/pause in tape mode
			if m.tapeMode && !m.showDetail {
				m.tapePlaying = !m.tapePlaying
				if m.tapePlaying {
					// Enable follow mode when playing starts
					m.followLatest = true
					if len(m.requests) > 0 {
						m.cursor = len(m.requests) - 1
					}
					// Record playback start for real-time mode
					m.tapePlayStart = time.Now()
					m.tapePlayStartPos = m.tape.CurrentTime
					if m.tapeRealtime {
						cmds = append(cmds, tapeRealtimeTickCmd())
					} else {
						cmds = append(cmds, tapePlayCmd())
					}
				}
			}

		case "r":
			// Toggle between realtime and step-through playback mode
			if m.tapeMode && !m.showDetail {
				m.tapeRealtime = !m.tapeRealtime
				// If currently playing, restart with new mode
				if m.tapePlaying {
					m.tapePlayStart = time.Now()
					m.tapePlayStartPos = m.tape.CurrentTime
				}
			}

		case "[":
			// Step backward in tape mode
			if m.tapeMode && !m.showDetail && m.tape != nil {
				m.tape.StepBackward()
				m.requests = m.tape.GetRequestsAtTime(m.tape.CurrentTime)
				m.tapePlaying = false
				// Follow to latest if in follow mode
				if m.followLatest && len(m.requests) > 0 {
					m.cursor = len(m.requests) - 1
				}
				if m.cursor >= len(m.requests) {
					m.cursor = max(0, len(m.requests)-1)
				}
			}

		case "]":
			// Step forward in tape mode
			if m.tapeMode && !m.showDetail && m.tape != nil {
				m.tape.StepForward()
				m.requests = m.tape.GetRequestsAtTime(m.tape.CurrentTime)
				m.tapePlaying = false
				// Follow to latest if in follow mode
				if m.followLatest && len(m.requests) > 0 {
					m.cursor = len(m.requests) - 1
				}
				if m.cursor >= len(m.requests) {
					m.cursor = max(0, len(m.requests)-1)
				}
			}

		case "-", "_":
			// Decrease playback speed
			if m.tapeMode && !m.showDetail {
				if m.tapeSpeed > 1 {
					m.tapeSpeed /= 2
				}
			}

		case "=", "+":
			// Increase playback speed
			if m.tapeMode && !m.showDetail {
				if m.tapeSpeed < 16 {
					m.tapeSpeed *= 2
				}
			}

		case "0":
			// Jump to start in tape mode (only when not entering a number prefix)
			if m.tapeMode && !m.showDetail && m.tape != nil && m.numBuffer == "" {
				m.tape.SeekToPercent(0)
				m.requests = m.tape.GetRequestsAtTime(m.tape.CurrentTime)
				m.tapePlaying = false
				m.cursor = 0
			}

		case "$":
			// Jump to end in tape mode
			if m.tapeMode && !m.showDetail && m.tape != nil {
				m.tape.SeekToPercent(1)
				m.requests = m.tape.GetRequestsAtTime(m.tape.CurrentTime)
				m.tapePlaying = false
				if len(m.requests) > 0 {
					m.cursor = len(m.requests) - 1
				}
			}

		case "up", "k":
			displayRequests := m.getDisplayRequests()
			if !m.showDetail && m.cursor > 0 {
				m.cursor -= count
				if m.cursor < 0 {
					m.cursor = 0
				}
				m.followLatest = false // Disable follow mode when manually navigating
			}
			// Clamp to display bounds
			if m.cursor >= len(displayRequests) {
				m.cursor = max(0, len(displayRequests)-1)
			}

		case "down", "j":
			displayRequests := m.getDisplayRequests()
			if !m.showDetail && m.cursor < len(displayRequests)-1 {
				m.cursor += count
				if m.cursor >= len(displayRequests) {
					m.cursor = len(displayRequests) - 1
				}
				m.followLatest = false // Disable follow mode when manually navigating
			}

		case "g", "home":
			if m.showDetail {
				// Scroll to top of viewport and first message
				m.viewport.GotoTop()
				m.currentMsgIndex = 0
			} else {
				// Jump to oldest (top) in list view
				m.cursor = 0
				m.followLatest = false
			}

		case "G", "end":
			displayRequests := m.getDisplayRequests()
			if m.showDetail {
				// Scroll to bottom of viewport and last message
				m.viewport.GotoBottom()
				if len(m.messagePositions) > 0 {
					m.currentMsgIndex = len(m.messagePositions) - 1
				}
			} else if len(displayRequests) > 0 {
				// Jump to newest (bottom) in list view
				m.cursor = len(displayRequests) - 1
				m.followLatest = false
			}

		case "f":
			// Toggle follow mode (auto-scroll to newest at bottom)
			// Works in both live mode and tape mode
			displayRequests := m.getDisplayRequests()
			if !m.showDetail {
				m.followLatest = !m.followLatest
				if m.followLatest && len(displayRequests) > 0 {
					m.cursor = len(displayRequests) - 1
				}
			}

		case "M":
			// Toggle mouse capture mode (Shift+M to allow text selection)
			m.mouseEnabled = !m.mouseEnabled
			if m.mouseEnabled {
				return m, tea.EnableMouseCellMotion
			}
			return m, tea.DisableMouse

		case "/":
			// Enter search mode
			if !m.showDetail {
				m.searchMode = true
			}

		case "enter":
			displayRequests := m.getDisplayRequests()
			if !m.showDetail && len(displayRequests) > 0 && m.cursor < len(displayRequests) {
				m.showDetail = true
				m.selected = displayRequests[m.cursor]
				m.selectedID = m.selected.ID
				m.activeTab = TabMessages
				// Reset message navigation state for new request
				m.currentMsgIndex = 0
				m.messagePositions = nil
				m.collapsedMessages = make(map[int]bool)
				m.viewport.SetContent(m.renderTabContent())
				m.viewport.GotoTop()
			}

		case "esc":
			if m.showDetail {
				m.showDetail = false
				m.selected = nil
			} else if m.searchQuery != "" {
				// Clear search filter
				m.searchQuery = ""
				m.filteredRequests = nil
				m.cursor = 0
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

	case "c":
			if m.showDetail {
				switch m.activeTab {
				case TabMessages:
					// Toggle collapse current message in Messages tab
					if m.collapsedMessages == nil {
						m.collapsedMessages = make(map[int]bool)
					}
					m.collapsedMessages[m.currentMsgIndex] = !m.collapsedMessages[m.currentMsgIndex]
					m.viewport.SetContent(m.renderTabContent())
				case TabOutput, TabRawInput, TabRawOutput:
					m.copyActiveTab()
				}
			}

		case "C":
			if m.showDetail {
				if m.activeTab == TabMessages {
					// Collapse/expand all messages in Messages tab
					if m.collapsedMessages == nil {
						m.collapsedMessages = make(map[int]bool)
					}
					// Check if any are expanded - if so, collapse all; otherwise expand all
					anyExpanded := false
					for i := 0; i < len(m.messagePositions); i++ {
						if !m.collapsedMessages[i] {
							anyExpanded = true
							break
						}
					}
					for i := 0; i < len(m.messagePositions); i++ {
						m.collapsedMessages[i] = anyExpanded
					}
					m.viewport.SetContent(m.renderTabContent())
				}
			}

		case "y":
			if m.showDetail && m.activeTab != TabMessages {
				m.copyInputOutput()
			}

		case "n":
			// Jump to next message in Messages/Output tab
			if m.showDetail && (m.activeTab == TabMessages || m.activeTab == TabOutput) {
				if m.currentMsgIndex < len(m.messagePositions)-1 {
					m.currentMsgIndex++
					if m.currentMsgIndex < len(m.messagePositions) {
						m.viewport.SetYOffset(m.messagePositions[m.currentMsgIndex])
					}
				}
			}

		case "N":
			// Jump to previous message in Messages/Output tab
			if m.showDetail && (m.activeTab == TabMessages || m.activeTab == TabOutput) {
				if m.currentMsgIndex > 0 {
					m.currentMsgIndex--
					if m.currentMsgIndex < len(m.messagePositions) {
						m.viewport.SetYOffset(m.messagePositions[m.currentMsgIndex])
					}
				}
			}

		case "J":
			// Jump to next request in detail view
			if m.showDetail {
				m.jumpToAdjacentRequest(1)
			}

		case "K":
			// Jump to previous request in detail view
			if m.showDetail {
				m.jumpToAdjacentRequest(-1)
			}
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.showDetail {
				// Handle tab clicks in detail view
				for i := 0; i < 4; i++ {
					tabZoneID := fmt.Sprintf("tab-%d", i)
					if zone.Get(tabZoneID).InBounds(msg) {
						m.activeTab = Tab(i)
						m.viewport.SetContent(m.renderTabContent())
						m.viewport.GotoTop()
						return m, nil
					}
				}

				// Handle clicks in Messages tab for collapsing messages using bubblezone
				// Only the header (role line) is clickable, so this works even when partially scrolled
				if m.activeTab == TabMessages {
					for i := 0; i < len(m.messagePositions); i++ {
						msgZoneID := fmt.Sprintf("msg-%d", i)
						if zone.Get(msgZoneID).InBounds(msg) {
							if m.collapsedMessages == nil {
								m.collapsedMessages = make(map[int]bool)
							}
							m.collapsedMessages[i] = !m.collapsedMessages[i]
							m.currentMsgIndex = i
							m.viewport.SetContent(m.renderTabContent())
							break
						}
					}

					// Handle clicks on image placeholders
					for _, img := range m.imageRefs {
						imgZoneID := fmt.Sprintf("img-%d", img.Index)
						if zone.Get(imgZoneID).InBounds(msg) {
							if err := openImage(img); err != nil {
								m.copyMessage = fmt.Sprintf("✗ Failed to open image: %v", err)
							} else {
								m.copyMessage = fmt.Sprintf("✓ Opened Image %d", img.Index)
							}
							m.copyMessageTime = time.Now()
							return m, nil
						}
					}
				}
			} else if !m.showDetail {
				// Handle sort header clicks
				sortHeaders := []struct {
					zoneID string
					field  SortField
				}{
					{"sort-id", SortByID},
					{"sort-status", SortByStatus},
					{"sort-model", SortByModel},
					{"sort-code", SortByCode},
					{"sort-size", SortBySize},
					{"sort-duration", SortByDuration},
					{"sort-intok", SortByInputTokens},
					{"sort-outtok", SortByOutputTokens},
					{"sort-cost", SortByCost},
				}

				for _, header := range sortHeaders {
					if zone.Get(header.zoneID).InBounds(msg) {
						m.toggleSort(header.field)
						return m, nil
					}
				}

				// Handle mouse clicks in list view
				// Header takes 5 lines (title + search + column headers + separator + blank)
				headerLines := 5
				clickedRow := msg.Y - headerLines
				listHeight := m.height - 9 // Adjusted for search bar

				displayRequests := m.getDisplayRequests()
				if clickedRow >= 0 && clickedRow < listHeight {
					// Calculate the actual index based on scroll position
					start := 0
					if len(displayRequests) > listHeight && m.cursor >= listHeight {
						start = m.cursor - listHeight + 1
					}

					actualIndex := start + clickedRow
					if actualIndex >= 0 && actualIndex < len(displayRequests) {
						m.cursor = actualIndex
						m.followLatest = false

						// Single click shows detail for better UX
						m.showDetail = true
						m.selected = displayRequests[m.cursor]
						m.selectedID = m.selected.ID
						m.activeTab = TabMessages
						m.viewport.SetContent(m.renderTabContent())
						m.viewport.GotoTop()
					}
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Detail view layout:
		// - Header line + 1 blank line = 2
		// - Tabs with border (3 lines) + 1 blank line = 4
		// - Viewport border (top + bottom) = 2
		// - Footer line = 1
		// Total overhead = 9 lines
		viewportHeight := m.height - 9

		// Pre-warm markdown renderer for message content width
		// contentWidth = m.width - 10, textWidth = contentWidth - 6 = m.width - 16
		InitMarkdownRenderer(m.width - 16)

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

	case tapeSavedMsg:
		m.saveMessage = fmt.Sprintf("✓ Saved to %s", msg.filename)
		m.saveMessageTime = time.Now()

	case tapeSaveErrorMsg:
		m.saveMessage = fmt.Sprintf("✗ Error: %v", msg.err)
		m.saveMessageTime = time.Now()

	case tapePlayMsg:
		// Step-through playback mode (advance event by event)
		if m.tapeMode && m.tapePlaying && m.tape != nil && !m.tapeRealtime {
			// Advance by speed multiplier
			for i := 0; i < m.tapeSpeed; i++ {
				if !m.tape.StepForward() {
					m.tapePlaying = false
					break
				}
			}
			m.requests = m.tape.GetRequestsAtTime(m.tape.CurrentTime)

			// Auto-follow to latest request when playing
			if m.followLatest && len(m.requests) > 0 {
				m.cursor = len(m.requests) - 1
			}
			// Clamp cursor to valid range
			if m.cursor >= len(m.requests) {
				m.cursor = max(0, len(m.requests)-1)
			}

			if m.tapePlaying && !m.tapeRealtime {
				cmds = append(cmds, tapePlayCmd())
			}
		}

	case tapeRealtimeTickMsg:
		// Real-time playback mode (play at actual recorded speed)
		if m.tapeMode && m.tapePlaying && m.tape != nil && m.tapeRealtime {
			// Calculate where we should be in the tape based on elapsed wall time
			elapsed := time.Since(m.tapePlayStart)
			// Apply speed multiplier to elapsed time
			scaledElapsed := time.Duration(float64(elapsed) * float64(m.tapeSpeed))
			targetTime := m.tapePlayStartPos.Add(scaledElapsed)

			// Don't go past the end
			if targetTime.After(m.tape.EndTime) {
				targetTime = m.tape.EndTime
				m.tapePlaying = false
			}

			m.tape.SeekToTime(targetTime)
			m.requests = m.tape.GetRequestsAtTime(m.tape.CurrentTime)

			// Auto-follow to latest request when playing
			if m.followLatest && len(m.requests) > 0 {
				m.cursor = len(m.requests) - 1
			}
			// Clamp cursor to valid range
			if m.cursor >= len(m.requests) {
				m.cursor = max(0, len(m.requests)-1)
			}

			if m.tapePlaying && m.tapeRealtime {
				cmds = append(cmds, tapeRealtimeTickCmd())
			}
		}

	case tickMsg:
		// In tape mode, don't sync with global state
		if m.tapeMode {
			cmds = append(cmds, tickCmd())
			// Clear save message after 3 seconds
			if m.saveMessage != "" && time.Since(m.saveMessageTime) > 3*time.Second {
				m.saveMessage = ""
			}
			if m.copyMessage != "" && time.Since(m.copyMessageTime) > 3*time.Second {
				m.copyMessage = ""
			}
			return m, tea.Batch(cmds...)
		}

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

		// Clear save message after 3 seconds
		if m.saveMessage != "" && time.Since(m.saveMessageTime) > 3*time.Second {
			m.saveMessage = ""
		}

		if m.copyMessage != "" && time.Since(m.copyMessageTime) > 3*time.Second {
			m.copyMessage = ""
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

// jumpToAdjacentRequest navigates to the next (direction=1) or previous (direction=-1) request in the detail view.
func (m *model) jumpToAdjacentRequest(direction int) {
	displayRequests := m.getDisplayRequests()
	if len(displayRequests) == 0 {
		return
	}

	// Find current request's index in the display list
	currentIdx := -1
	for i, req := range displayRequests {
		if req.ID == m.selectedID {
			currentIdx = i
			break
		}
	}

	newIdx := currentIdx + direction
	if newIdx < 0 || newIdx >= len(displayRequests) {
		return
	}

	m.cursor = newIdx
	m.selected = displayRequests[newIdx]
	m.selectedID = m.selected.ID
	m.currentMsgIndex = 0
	m.messagePositions = nil
	m.collapsedMessages = make(map[int]bool)
	m.viewport.SetContent(m.renderTabContent())
	m.viewport.GotoTop()
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.showSaveDialog {
		return m.renderSaveDialog()
	}

	if m.showDetail {
		return zone.Scan(m.renderDetailView())
	}

	return m.renderListView()
}
