package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Config variables for flags
var (
	configFile           string
	port                 int
	targetURL            string
	saveTape             string
	cacheMode            string
	cacheTTL             time.Duration
	cacheSimulateLatency bool
	cacheDir             string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "llmproxy-go",
	Short: "LLM API Proxy with TUI",
	Long: `llmproxy-go is a powerful LLM API proxy that provides a TUI for
inspecting and debugging API requests and responses.

Examples:
  llmproxy-go                              Start with defaults (:115 → api.openai.com)
  llmproxy-go -p 9000 -t http://api.com    Proxy from :9000 to api.com
  llmproxy-go -c config.toml               Start with configuration file
  llmproxy-go replay session.tape          Replay a recorded tape file
  llmproxy-go cost session.tape            Show cost breakdown for a tape`,
	Run: func(cmd *cobra.Command, args []string) {
		// Check if using config file mode
		if configFile != "" {
			runWithConfig(configFile)
			return
		}

		// Normal single-proxy mode
		runSingleProxy()
	},
}

// replayCmd represents the replay command
var replayCmd = &cobra.Command{
	Use:   "replay <tape-file>",
	Short: "Replay a recorded tape file for inspection",
	Long: `Open a previously recorded tape file in the TUI for inspection.
Tape files contain recorded API requests and responses.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		tapeFile := args[0]
		tape, err := LoadTape(tapeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading tape: %v\n", err)
			os.Exit(1)
		}

		// Suppress log output during TUI operation to prevent layout issues
		log.SetOutput(io.Discard)

		// Start the TUI in tape mode
		program = tea.NewProgram(
			initialTapeModel(tape),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)

		if _, err := program.Run(); err != nil {
			log.Fatalf("Error running TUI: %v", err)
		}
	},
}

// costCmd represents the cost command
var costCmd = &cobra.Command{
	Use:   "cost <tape-file>",
	Short: "Print cost breakdown for a tape file",
	Long:  `Analyze a tape file and print a detailed cost breakdown of all API calls.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		RunCostCommand(args[0])
	},
}

// genConfigCmd represents the gen-config command
var genConfigCmd = &cobra.Command{
	Use:   "gen-config",
	Short: "Generate an example configuration file",
	Long:  `Print an example TOML configuration file to stdout.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(GenerateExampleConfig())
	},
}

func init() {
	// Root command flags
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to TOML config file for multi-proxy configuration")
	rootCmd.Flags().IntVarP(&port, "port", "p", 115, "Port to listen on")
	rootCmd.Flags().StringVarP(&targetURL, "target", "t", "https://api.openai.com", "Target URL to proxy to")
	rootCmd.Flags().StringVarP(&saveTape, "save-tape", "s", "", "Auto-save session to tape file")
	rootCmd.Flags().StringVarP(&cacheMode, "cache", "m", "none", "Cache mode: none, memory, global")
	rootCmd.Flags().DurationVar(&cacheTTL, "cache-ttl", 24*time.Hour, "Cache TTL duration (e.g., 1h, 24h)")
	rootCmd.Flags().BoolVar(&cacheSimulateLatency, "cache-simulate-latency", false, "Simulate original response latency for cached responses")
	rootCmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Directory for badger cache (default: ~/.llmproxy-cache)")

	// Add subcommands
	rootCmd.AddCommand(replayCmd)
	rootCmd.AddCommand(costCmd)
	rootCmd.AddCommand(genConfigCmd)
}

func main() {
	// Clean up any temp images on exit
	defer cleanupTempImages()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
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

	// Suppress log output during TUI operation to prevent layout issues
	log.SetOutput(io.Discard)

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

// runSingleProxy runs the proxy server with command-line flags
func runSingleProxy() {
	// Initialize cache
	badgerPath := cacheDir
	if badgerPath == "" {
		home, _ := os.UserHomeDir()
		badgerPath = filepath.Join(home, ".llmproxy-cache")
	}

	cacheConfig := CacheConfig{
		Mode:            CacheMode(cacheMode),
		TTL:             cacheTTL,
		SimulateLatency: cacheSimulateLatency,
		BadgerPath:      badgerPath,
	}

	if err := InitCache(cacheConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing cache: %v\n", err)
		os.Exit(1)
	}
	defer CloseCache()

	// Format listen address from port
	listenAddr := fmt.Sprintf(":%d", port)

	// Initialize tape writer if save-tape is specified
	if saveTape != "" {
		writer, err := NewTapeWriter(saveTape)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tape file: %v\n", err)
			os.Exit(1)
		}
		tapeWriter = writer
		tapeWriter.WriteSessionStart(listenAddr, targetURL)
		defer func() {
			if tapeWriter != nil {
				tapeWriter.Close()
			}
		}()
	}

	// Start the proxy server
	startProxy(listenAddr, targetURL)

	// Suppress log output during TUI operation to prevent layout issues
	log.SetOutput(io.Discard)

	// Start the TUI
	program = tea.NewProgram(
		initialModel(listenAddr, targetURL, saveTape),
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
