package main

import (
	"flag"
	"log"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "Address to listen on")
	targetURL := flag.String("target", "http://localhost:3000", "Target URL to proxy to")
	flag.Parse()

	// Start the proxy server
	startProxy(*listenAddr, *targetURL)

	// Start the TUI
	program = tea.NewProgram(
		initialModel(*listenAddr, *targetURL),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := program.Run(); err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}
}
