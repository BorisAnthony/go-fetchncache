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
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"gopkg.in/yaml.v3"
)

var version = "dev" // Will be overridden by build flags

// PathConfig represents a dynamic path configuration
type PathConfig struct {
	String  string `yaml:"string"`
	Pattern string `yaml:"pattern"`
}

// Target represents a URL target from the YAML config
type Target struct {
	Name    string      `yaml:"name"`
	URL     string      `yaml:"url"`
	Path    interface{} `yaml:"path"` // Can be string or []PathConfig
	Headers []string    `yaml:"headers,omitempty"`
}

// Config represents the YAML configuration structure
type Config struct {
	LogFile string   `yaml:"logfile"`
	Targets []Target `yaml:"targets"`
}

// generatePatternValue generates a timestamp string based on the pattern
func generatePatternValue(pattern string) (string, error) {
	parts := strings.Split(pattern, "-")
	if len(parts) != 3 {
		return "", fmt.Errorf("pattern must have 3 components: DateTime-Timezone-Processing")
	}

	// 1. Generate timestamp
	now := time.Now()

	// 2. Apply timezone
	if parts[1] == "UTC" {
		now = now.UTC()
	} else {
		loc, err := time.LoadLocation(parts[1])
		if err != nil {
			return "", fmt.Errorf("loading timezone %q: %w", parts[1], err)
		}
		now = now.In(loc)
	}

	// 3. Format datetime using Go time layout constants
	var layout string
	switch parts[0] {
	case "DateTime":
		layout = time.DateTime // "2006-01-02 15:04:05"
	case "DateOnly":
		layout = time.DateOnly // "2006-01-02"
	case "TimeOnly":
		layout = time.TimeOnly // "15:04:05"
	case "RFC3339":
		layout = time.RFC3339 // "2006-01-02T15:04:05Z07:00"
	case "Kitchen":
		layout = time.Kitchen // "3:04PM"
	case "Stamp":
		layout = time.Stamp // "Jan _2 15:04:05"
	case "DATETIME_SIMPLE_FS":
		layout = "2006-01-02 1504" // "2006-01-02 1504"
	default:
		return "", fmt.Errorf("unsupported datetime format: %s (supported: DateTime, DateOnly, TimeOnly, RFC3339, Kitchen, Stamp, DATETIME_SIMPLE_FS)", parts[0])
	}
	
	formatted := now.Format(layout)

	// 4. Apply processing
	switch parts[2] {
	case "slug":
		// Make filename-safe: replace spaces and colons, convert to lowercase
		// formatted = strings.ReplaceAll(formatted, " ", "-")
		formatted = strings.ReplaceAll(formatted, ":", "-")
		// formatted = strings.ToLower(formatted + "-" + strings.ToLower(parts[1]))
	default:
		return "", fmt.Errorf("unsupported processing: %s", parts[2])
	}

	return formatted, nil
}

// GetResolvedPath resolves the target path, handling both static strings and dynamic patterns
func (t *Target) GetResolvedPath() (string, error) {
	switch v := t.Path.(type) {
	case string:
		// Legacy string path
		return v, nil
	case []interface{}:
		// New pattern-based path
		if len(v) != 1 {
			return "", fmt.Errorf("path array must contain exactly one configuration object")
		}

		configMap, ok := v[0].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("path configuration must be an object")
		}

		template, hasString := configMap["string"].(string)
		pattern, hasPattern := configMap["pattern"].(string)

		if !hasString || !hasPattern {
			return "", fmt.Errorf("path configuration must have 'string' and 'pattern' fields")
		}

		if !strings.Contains(template, "{pattern}") {
			return "", fmt.Errorf("path template must contain {pattern} placeholder")
		}

		// Generate pattern value
		patternValue, err := generatePatternValue(pattern)
		if err != nil {
			return "", fmt.Errorf("generating pattern value: %w", err)
		}

		// Replace placeholder with generated value
		resolvedPath := strings.ReplaceAll(template, "{pattern}", patternValue)
		return resolvedPath, nil
	default:
		return "", fmt.Errorf("path must be string or configuration object")
	}
}

// IsStaticPath returns true if this target uses a static string path
func (t *Target) IsStaticPath() bool {
	_, ok := t.Path.(string)
	return ok
}

// generateLatestPath creates a "latest" version of a file path
// For example: "./cache/data.json" -> "./cache/latest.json"
//             "./cache/data-timestamp.json" -> "./cache/latest.json"
//             "./cache/data.pp.json" -> "./cache/latest.pp.json"
func generateLatestPath(resolvedPath string) string {
	dir := filepath.Dir(resolvedPath)
	filename := filepath.Base(resolvedPath)
	
	// Handle different file extension patterns
	if strings.Contains(filename, ".pp.json") {
		return filepath.Join(dir, "latest.pp.json")
	} else if strings.HasSuffix(filename, ".json") {
		return filepath.Join(dir, "latest.json")
	}
	
	// For other extensions, use the original logic
	ext := filepath.Ext(resolvedPath)
	if ext == "" {
		return filepath.Join(dir, "latest")
	}
	
	return filepath.Join(dir, "latest"+ext)
}

// parseFlags parses and validates command line flags
func parseFlags() (string, bool, string, bool, int) {
	var configPath, jsonFormat string
	var verbose, showVersion, latest bool
	var delay int

	flag.StringVar(&configPath, "config", "", "Path to YAML config file")
	flag.BoolVar(&verbose, "v", false, "Enable verbose mode")
	flag.StringVar(&jsonFormat, "json-format", "original", "JSON formatting: 'original', 'pretty', 'minimized', or 'both'")
	flag.BoolVar(&latest, "latest", false, "Create a 'latest' copy of each downloaded file")
	flag.IntVar(&delay, "d", 0, "Delay in seconds between targets")
	flag.IntVar(&delay, "delay", 0, "Delay in seconds between targets")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.Parse()

	if showVersion {
		fmt.Printf("fetchncache version %s\n", version)
		os.Exit(0)
	}

	if configPath == "" {
		fmt.Fprintf(os.Stderr, "Error: --config flag is required\n")
		fmt.Fprintf(os.Stderr, "Usage: %s --config <yaml-file> [-v] [-d|--delay <seconds>] [--json-format original|pretty|minimized|both] [--latest] [--version]\n", os.Args[0])
		os.Exit(1)
	}

	if delay < 0 {
		fmt.Fprintf(os.Stderr, "Error: delay must be non-negative\n")
		os.Exit(1)
	}

	validFormats := []string{"original", "pretty", "minimized", "both"}
	for _, format := range validFormats {
		if jsonFormat == format {
			return configPath, verbose, jsonFormat, latest, delay
		}
	}

	fmt.Fprintf(os.Stderr, "Error: --json-format must be one of: %s\n", strings.Join(validFormats, ", "))
	os.Exit(1)
	return "", false, "", false, 0 // unreachable
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

// validatePathConfig validates a path configuration (string or pattern-based)
func validatePathConfig(pathConfig interface{}) error {
	switch v := pathConfig.(type) {
	case string:
		// Legacy string path - always valid if not empty
		if v == "" {
			return fmt.Errorf("path cannot be empty")
		}
		return nil
	case []interface{}:
		if len(v) != 1 {
			return fmt.Errorf("path array must contain exactly one configuration object")
		}

		configMap, ok := v[0].(map[string]interface{})
		if !ok {
			return fmt.Errorf("path configuration must be an object")
		}

		template, hasString := configMap["string"].(string)
		pattern, hasPattern := configMap["pattern"].(string)

		if !hasString || !hasPattern {
			return fmt.Errorf("path configuration must have 'string' and 'pattern' fields")
		}

		if template == "" {
			return fmt.Errorf("path template cannot be empty")
		}

		if !strings.Contains(template, "{pattern}") {
			return fmt.Errorf("path template must contain {pattern} placeholder")
		}

		return validatePattern(pattern)
	default:
		return fmt.Errorf("path must be string or configuration object")
	}
}

// validatePattern validates a pattern string format
func validatePattern(pattern string) error {
	parts := strings.Split(pattern, "-")
	if len(parts) != 3 {
		return fmt.Errorf("pattern must have 3 components: DateTime-Timezone-Processing")
	}

	// Validate datetime format component
	switch parts[0] {
	case "DateTime", "DateOnly", "TimeOnly", "RFC3339", "Kitchen", "Stamp", "DATETIME_SIMPLE_FS":
		// Valid datetime formats
	default:
		return fmt.Errorf("unsupported datetime format: %s (supported: DateTime, DateOnly, TimeOnly, RFC3339, Kitchen, Stamp, DATETIME_SIMPLE_FS)", parts[0])
	}

	// Validate timezone component
	if parts[1] != "UTC" {
		_, err := time.LoadLocation(parts[1])
		if err != nil {
			return fmt.Errorf("invalid timezone %q: %w", parts[1], err)
		}
	}

	switch parts[2] {
	case "slug":
		// Valid processing
	default:
		return fmt.Errorf("unsupported processing: %s (supported: slug)", parts[2])
	}

	return nil
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
		if target.Path == nil {
			return fmt.Errorf("target %d: Path is required", i+1)
		}

		// Validate path format
		if err := validatePathConfig(target.Path); err != nil {
			return fmt.Errorf("target %d: %w", i+1, err)
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
func formatJSON(data []byte, format string, targetPath string, latest bool) ([]byte, string, error) {
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
		return formatJSONBoth(jsonData, targetPath, latest)

	default:
		return data, "", fmt.Errorf("unknown format: %s", format)
	}
}

// formatJSONBoth creates both minimized and pretty-printed versions
func formatJSONBoth(jsonData any, targetPath string, latest bool) ([]byte, string, error) {
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

	// If latest flag is set, also create latest.pp.json
	if latest {
		latestPrettyPath := generateLatestPath(prettyPath)
		if err := writeFileWithDir(latestPrettyPath, pretty); err != nil {
			return minimized, "minimized", fmt.Errorf("writing latest pretty file: %w", err)
		}
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
func processTarget(target Target, client *retryablehttp.Client, jsonFormat string, latest bool, fileLogger, consoleLogger *slog.Logger) error {
	// Resolve path first
	resolvedPath, err := target.GetResolvedPath()
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	if consoleLogger != nil {
		consoleLogger.Info("Processing target", "name", target.Name, "url", target.URL, "path", resolvedPath)
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
	dataToWrite, formatDesc, err := formatJSON(bodyBytes, jsonFormat, resolvedPath, latest)
	if err != nil {
		fileLogger.Warn("Could not format JSON, using original", "path", resolvedPath, "error", err)
		dataToWrite = bodyBytes
	} else if formatDesc != "" && consoleLogger != nil {
		consoleLogger.Info("Formatted JSON", "format", formatDesc)
	}

	// Write file
	if err := writeFileWithDir(resolvedPath, dataToWrite); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	if consoleLogger != nil {
		consoleLogger.Info("Successfully wrote file", "path", resolvedPath)
	}

	// Write latest file if flag is set
	if latest {
		latestPath := generateLatestPath(resolvedPath)
		if err := writeFileWithDir(latestPath, dataToWrite); err != nil {
			// Log warning but don't fail the entire operation
			fileLogger.Warn("Failed to write latest file", "path", latestPath, "error", err)
			if consoleLogger != nil {
				consoleLogger.Warn("Failed to write latest file", "path", latestPath, "error", err)
			}
		} else if consoleLogger != nil {
			consoleLogger.Info("Successfully wrote latest file", "path", latestPath)
		}
	}

	return nil
}

func main() {
	// Parse command line flags
	configPath, verbose, jsonFormat, latest, delay := parseFlags()

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

		if err := processTarget(target, retryClient, jsonFormat, latest, fileLogger, consoleLogger); err != nil {
			fileLogger.Error("Failed to process target", "name", target.Name, "url", target.URL, "error", err)
		}

		// Add delay between targets (but not after the last one)
		if delay > 0 && i < len(config.Targets)-1 {
			if consoleLogger != nil {
				consoleLogger.Info("Waiting before next target", "delay_seconds", delay)
			}
			time.Sleep(time.Duration(delay) * time.Second)
		}
	}

	if consoleLogger != nil {
		consoleLogger.Info("Application finished successfully!")
	}
}
