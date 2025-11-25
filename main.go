package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "Address to listen on")
	targetURL := flag.String("target", "http://localhost:3000", "Target URL to proxy to")
	tapeFile := flag.String("tape", "", "Open a tape file for inspection (replay mode)")
	saveTape := flag.String("save-tape", "", "Auto-save session to tape file")

	// Cache configuration flags
	cacheMode := flag.String("cache", "none", "Cache mode: none, memory, global")
	cacheTTL := flag.Duration("cache-ttl", 24*time.Hour, "Cache TTL duration (e.g., 1h, 24h, 7d)")
	cacheSimulateLatency := flag.Bool("cache-simulate-latency", false, "Simulate original response latency for cached responses")
	cacheDir := flag.String("cache-dir", "", "Directory for badger cache (default: ~/.llmproxy-cache)")

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
	// Initialize cache
	badgerPath := *cacheDir
	if badgerPath == "" {
		home, _ := os.UserHomeDir()
		badgerPath = filepath.Join(home, ".llmproxy-cache")
	}

	cacheConfig := CacheConfig{
		Mode:            CacheMode(*cacheMode),
		TTL:             *cacheTTL,
		SimulateLatency: *cacheSimulateLatency,
		BadgerPath:      badgerPath,
	}

	if err := InitCache(cacheConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing cache: %v\n", err)
		os.Exit(1)
	}
	defer CloseCache()

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
