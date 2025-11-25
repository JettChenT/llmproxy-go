package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "Address to listen on")
	targetURL := flag.String("target", "http://localhost:3000", "Target URL to proxy to")
	tapeFile := flag.String("tape", "", "Open a tape file for inspection (replay mode)")
	saveTape := flag.String("save-tape", "", "Auto-save session to tape file")
	flag.Parse()

	// Check if we're in tape playback mode
	if *tapeFile != "" {
		tape, err := LoadTape(*tapeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading tape: %v\n", err)
			os.Exit(1)
		}

		// Start the TUI in tape mode
		program = tea.NewProgram(
			initialTapeModel(tape),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)

		if _, err := program.Run(); err != nil {
			log.Fatalf("Error running TUI: %v", err)
		}
		return
	}

	// Normal proxy mode
	// Initialize tape writer if save-tape is specified
	if *saveTape != "" {
		writer, err := NewTapeWriter(*saveTape)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tape file: %v\n", err)
			os.Exit(1)
		}
		tapeWriter = writer
		tapeWriter.WriteSessionStart(*listenAddr, *targetURL)
		defer func() {
			if tapeWriter != nil {
				tapeWriter.Close()
			}
		}()
	}

	// Start the proxy server
	startProxy(*listenAddr, *targetURL)

	// Start the TUI
	program = tea.NewProgram(
		initialModel(*listenAddr, *targetURL, *saveTape),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := program.Run(); err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}
}
