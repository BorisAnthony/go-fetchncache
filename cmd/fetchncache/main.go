package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
	"gopkg.in/yaml.v3"
)

var version = "dev" // Will be overridden by build flags

// Target represents a URL target from the YAML config
type Target struct {
	Name    string   `yaml:"name"`
	URL     string   `yaml:"url"`
	Path    string   `yaml:"path"`
	Headers []string `yaml:"headers,omitempty"`
}

// Config represents the YAML configuration structure
type Config struct {
	LogFile string   `yaml:"logfile"`
	Targets []Target `yaml:"targets"`
}

// parseFlags parses and validates command line flags
func parseFlags() (string, bool, string) {
	var configPath, jsonFormat string
	var verbose, showVersion bool

	flag.StringVar(&configPath, "config", "", "Path to YAML config file")
	flag.BoolVar(&verbose, "v", false, "Enable verbose mode")
	flag.StringVar(&jsonFormat, "json-format", "original", "JSON formatting: 'original', 'pretty', 'minimized', or 'both'")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.Parse()

	if showVersion {
		fmt.Printf("fetchncache version %s\n", version)
		os.Exit(0)
	}

	if configPath == "" {
		fmt.Fprintf(os.Stderr, "Error: --config flag is required\n")
		fmt.Fprintf(os.Stderr, "Usage: %s --config <yaml-file> [-v] [--json-format original|pretty|minimized|both] [--version]\n", os.Args[0])
		os.Exit(1)
	}

	validFormats := []string{"original", "pretty", "minimized", "both"}
	for _, format := range validFormats {
		if jsonFormat == format {
			return configPath, verbose, jsonFormat
		}
	}

	fmt.Fprintf(os.Stderr, "Error: --json-format must be one of: %s\n", strings.Join(validFormats, ", "))
	os.Exit(1)
	return "", false, "" // unreachable
}

// loadConfig reads and parses the YAML configuration file
func loadConfig(path string) (Config, error) {
	var config Config

	configData, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("reading config file: %w", err)
	}

	err = yaml.Unmarshal(configData, &config)
	if err != nil {
		return config, fmt.Errorf("parsing YAML config: %w", err)
	}

	// Validate config
	if err := validateConfig(config); err != nil {
		return config, fmt.Errorf("invalid config: %w", err)
	}

	return config, nil
}

// validateConfig validates the configuration structure
func validateConfig(config Config) error {
	if len(config.Targets) == 0 {
		return fmt.Errorf("no targets specified in config")
	}

	for i, target := range config.Targets {
		if target.URL == "" {
			return fmt.Errorf("target %d: URL is required", i+1)
		}
		if target.Path == "" {
			return fmt.Errorf("target %d: Path is required", i+1)
		}

		// Validate URL format
		if _, err := url.Parse(target.URL); err != nil {
			return fmt.Errorf("target %d: invalid URL %q: %w", i+1, target.URL, err)
		}

		// Validate headers format
		if _, err := parseHeaders(target.Headers); err != nil {
			return fmt.Errorf("target %d: %w", i+1, err)
		}
	}

	return nil
}

// parseHeaders parses header strings in "name: value" format into http.Header
func parseHeaders(headerStrings []string) (http.Header, error) {
	headers := make(http.Header)
	for _, headerStr := range headerStrings {
		if headerStr == "" {
			continue // Skip empty header strings
		}

		parts := strings.SplitN(headerStr, ": ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header format: %q (expected 'name: value')", headerStr)
		}

		name := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if name == "" {
			return nil, fmt.Errorf("empty header name in: %q", headerStr)
		}

		headers.Set(name, value)
	}
	return headers, nil
}

// setupLoggers creates and configures file and console loggers
func setupLoggers(config Config, verbose bool) (*slog.Logger, *slog.Logger, func(), error) {
	var fileLogger *slog.Logger
	var consoleLogger *slog.Logger
	var cleanup func()

	// Setup file logger
	if config.LogFile != "" {
		logDir := filepath.Dir(config.LogFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, nil, nil, fmt.Errorf("creating log directory %q: %w", logDir, err)
		}

		logFile, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("opening log file %q: %w", config.LogFile, err)
		}

		fileLogger = slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))

		cleanup = func() { logFile.Close() }
	} else {
		fileLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
		cleanup = func() {}
	}

	// Setup console logger
	if verbose {
		consoleLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}

	return fileLogger, consoleLogger, cleanup, nil
}

// formatJSON handles JSON formatting based on the specified format
func formatJSON(data []byte, format string, targetPath string) ([]byte, string, error) {
	// Skip formatting if not JSON or format is original
	if !strings.HasSuffix(strings.ToLower(targetPath), ".json") || format == "original" {
		return data, "", nil
	}

	var jsonData any
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return data, "", fmt.Errorf("parsing JSON: %w", err)
	}

	switch format {
	case "pretty":
		formatted, err := json.MarshalIndent(jsonData, "", "  ")
		return formatted, "pretty-printed", err

	case "minimized":
		formatted, err := json.Marshal(jsonData)
		return formatted, "minimized", err

	case "both":
		return formatJSONBoth(jsonData, targetPath)

	default:
		return data, "", fmt.Errorf("unknown format: %s", format)
	}
}

// formatJSONBoth creates both minimized and pretty-printed versions
func formatJSONBoth(jsonData any, targetPath string) ([]byte, string, error) {
	minimized, err := json.Marshal(jsonData)
	if err != nil {
		return nil, "", err
	}

	pretty, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		return minimized, "minimized", nil
	}

	prettyPath := strings.TrimSuffix(targetPath, ".json") + ".pp.json"
	if err := writeFileWithDir(prettyPath, pretty); err != nil {
		return minimized, "minimized", fmt.Errorf("writing pretty file: %w", err)
	}

	return minimized, "minimized (with pretty version)", nil
}

// writeFileWithDir creates the directory if needed and writes the file
func writeFileWithDir(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %q: %w", dir, err)
	}

	return os.WriteFile(path, data, 0644)
}

// processTarget processes a single target
func processTarget(target Target, client *retryablehttp.Client, jsonFormat string, fileLogger, consoleLogger *slog.Logger) error {
	if consoleLogger != nil {
		consoleLogger.Info("Processing target", "name", target.Name, "url", target.URL)
	}

	// Create HTTP request
	req, err := retryablehttp.NewRequest("GET", target.URL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// Set custom headers if specified
	if len(target.Headers) > 0 {
		customHeaders, err := parseHeaders(target.Headers)
		if err != nil {
			return fmt.Errorf("parsing headers: %w", err)
		}

		// Copy custom headers to the request
		for name, values := range customHeaders {
			for _, value := range values {
				req.Header.Set(name, value)
			}
		}

		if consoleLogger != nil {
			consoleLogger.Info("Set custom headers", "count", len(customHeaders))
		}
	}

	// Fetch data
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if consoleLogger != nil {
		consoleLogger.Info("Successfully fetched data", "bytes", len(bodyBytes))
	}

	// Format JSON if needed
	dataToWrite, formatDesc, err := formatJSON(bodyBytes, jsonFormat, target.Path)
	if err != nil {
		fileLogger.Warn("Could not format JSON, using original", "path", target.Path, "error", err)
		dataToWrite = bodyBytes
	} else if formatDesc != "" && consoleLogger != nil {
		consoleLogger.Info("Formatted JSON", "format", formatDesc)
	}

	// Write file
	if err := writeFileWithDir(target.Path, dataToWrite); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	if consoleLogger != nil {
		consoleLogger.Info("Successfully wrote file", "path", target.Path)
	}

	return nil
}

func main() {
	// Parse command line flags
	configPath, verbose, jsonFormat := parseFlags()

	// Load configuration
	config, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Setup loggers
	fileLogger, consoleLogger, cleanup, err := setupLoggers(config, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up loggers: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	if consoleLogger != nil {
		consoleLogger.Info("Reading config file", "path", configPath)
		consoleLogger.Info("Found targets to process", "count", len(config.Targets))
	}

	// Create HTTP client
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 3
	if !verbose {
		retryClient.Logger = nil
	}

	// Process each target
	for i, target := range config.Targets {
		if consoleLogger != nil {
			consoleLogger.Info("Processing target", "index", i+1, "total", len(config.Targets))
		}

		if err := processTarget(target, retryClient, jsonFormat, fileLogger, consoleLogger); err != nil {
			fileLogger.Error("Failed to process target", "name", target.Name, "url", target.URL, "error", err)
			continue
		}
	}

	if consoleLogger != nil {
		consoleLogger.Info("Application finished successfully!")
	}
}
