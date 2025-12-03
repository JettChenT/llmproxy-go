package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Define flags at package level so they're available for help
var (
	configFile           = flag.String("config", "", "Path to TOML config file for multi-proxy configuration")
	listenAddr           = flag.String("listen", ":8080", "Address to listen on")
	targetURL            = flag.String("target", "http://localhost:3000", "Target URL to proxy to")
	tapeFile             = flag.String("tape", "", "Open a tape file for inspection (replay mode)")
	saveTape             = flag.String("save-tape", "", "Auto-save session to tape file")
	cacheMode            = flag.String("cache", "none", "Cache mode: none, memory, global")
	cacheTTL             = flag.Duration("cache-ttl", 24*time.Hour, "Cache TTL duration (e.g., 1h, 24h, 7d)")
	cacheSimulateLatency = flag.Bool("cache-simulate-latency", false, "Simulate original response latency for cached responses")
	cacheDir             = flag.String("cache-dir", "", "Directory for badger cache (default: ~/.llmproxy-cache)")
	genConfig            = flag.Bool("gen-config", false, "Generate an example configuration file and exit")
)

func printUsage() {
	fmt.Fprintf(os.Stderr, `llmproxy-go - LLM API Proxy with TUI

Usage:
  llmproxy-go [flags]              Start the proxy server
  llmproxy-go --config config.toml Start with configuration file (multiple proxies)
  llmproxy-go --gen-config         Print example configuration to stdout
  llmproxy-go cost <tape-file>     Print cost breakdown for a tape file

Flags:
`)
	flag.PrintDefaults()
}

func main() {
	flag.Usage = printUsage

	// Check for subcommands first
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "cost":
			if len(os.Args) < 3 {
				fmt.Fprintf(os.Stderr, "Usage: llmproxy-go cost <tape-file>\n")
				os.Exit(1)
			}
			RunCostCommand(os.Args[2])
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	flag.Parse()

	// Generate example config and exit
	if *genConfig {
		fmt.Print(GenerateExampleConfig())
		return
	}

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

	// Check if using config file mode
	if *configFile != "" {
		runWithConfig(*configFile)
		return
	}

	// Normal single-proxy mode (legacy flags)
	runSingleProxy()
}

// runWithConfig runs the proxy server using a TOML configuration file
func runWithConfig(configPath string) {
	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Initialize cache from config
	cacheConfig, err := config.Cache.ToCacheConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing cache config: %v\n", err)
		os.Exit(1)
	}

	if err := InitCache(cacheConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing cache: %v\n", err)
		os.Exit(1)
	}
	defer CloseCache()

	// Initialize tape writer if specified in config
	saveTapeFile := config.SaveTape
	if saveTapeFile != "" {
		writer, err := NewTapeWriter(saveTapeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tape file: %v\n", err)
			os.Exit(1)
		}
		tapeWriter = writer
		// For multi-proxy, write session start with all proxies info
		proxySummary := formatProxySummary(config.Proxies)
		tapeWriter.WriteSessionStart(proxySummary, "multi-proxy")
		defer func() {
			if tapeWriter != nil {
				tapeWriter.Close()
			}
		}()
	}

	// Start all proxy instances
	if err := StartMultipleProxies(config.Proxies); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting proxies: %v\n", err)
		os.Exit(1)
	}

	// Build display strings for TUI
	listenAddrs := formatListenAddrs(config.Proxies)
	targetURLs := formatTargetURLs(config.Proxies)

	// Start the TUI
	program = tea.NewProgram(
		initialModel(listenAddrs, targetURLs, saveTapeFile),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := program.Run(); err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}
}

// runSingleProxy runs the proxy server with command-line flags (legacy mode)
func runSingleProxy() {
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

// formatProxySummary creates a summary string of all proxies
func formatProxySummary(proxies []ProxyConfig) string {
	var parts []string
	for _, p := range proxies {
		name := p.Name
		if name == "" {
			name = p.Listen
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", name, p.Listen))
	}
	return strings.Join(parts, ", ")
}

// formatListenAddrs creates a display string of all listen addresses
// In multi-proxy mode, returns "multi" to signal the view to use a different format
func formatListenAddrs(proxies []ProxyConfig) string {
	if len(proxies) == 1 {
		return proxies[0].Listen
	}
	return "multi"
}

// formatTargetURLs creates a display string of all target URLs
// In multi-proxy mode, returns a formatted string showing each proxy as name(port→host)
func formatTargetURLs(proxies []ProxyConfig) string {
	if len(proxies) == 1 {
		return proxies[0].Target
	}
	// In multi-proxy mode, show each proxy as a complete unit: name(port→host)
	var parts []string
	for _, p := range proxies {
		// Extract just the host from the URL for brevity
		target := p.Target
		target = strings.TrimPrefix(target, "https://")
		target = strings.TrimPrefix(target, "http://")
		target = strings.TrimSuffix(target, "/")
		// Truncate long hostnames
		if len(target) > 25 {
			target = target[:22] + "…"
		}

		name := p.Name
		if name == "" {
			name = p.Listen
		}
		parts = append(parts, fmt.Sprintf("%s(%s→%s)", name, p.Listen, target))
	}
	return strings.Join(parts, " ")
}
