package main

import (
	"encoding/json" // Package for JSON encoding and decoding
	"flag"          // Package for command line flag parsing
	"io"            // Package for I/O primitives, like reading response bodies
	"log/slog"      // Package for structured logging
	"net/http"      // Package for making HTTP requests
	"os"            // Package for operating system functionality, like file operations
	"path/filepath" // Package for file path manipulation
	"strings"       // Package for string manipulation

	"github.com/hashicorp/go-retryablehttp" // Package for HTTP requests with retry logic
	"gopkg.in/yaml.v3"                      // Package for YAML parsing
)

// Target represents a URL target from the YAML config
type Target struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	Path string `yaml:"path"`
}

// Config represents the YAML configuration structure
type Config struct {
	LogFile string   `yaml:"logfile"`
	Targets []Target `yaml:"targets"`
}

func main() {
	// Setup basic logger for startup errors (before config is loaded)
	startupLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Parse command line flags
	var configPath string
	var verbose bool
	var jsonFormat string

	flag.StringVar(&configPath, "config", "", "Path to YAML config file")
	flag.BoolVar(&verbose, "v", false, "Enable verbose mode")
	flag.StringVar(&jsonFormat, "json-format", "original", "JSON formatting: 'original', 'pretty', 'minimized', or 'both'")
	flag.Parse()

	if configPath == "" {
		startupLogger.Error("--config flag is required")
		startupLogger.Error("Usage", "command", os.Args[0]+" --config <yaml-file> [-v] [--json-format original|pretty|minimized|both]")
		os.Exit(1)
	}

	// Validate json-format flag
	if jsonFormat != "original" && jsonFormat != "pretty" && jsonFormat != "minimized" && jsonFormat != "both" {
		startupLogger.Error("--json-format must be 'original', 'pretty', 'minimized', or 'both'")
		os.Exit(1)
	}

	// Read and parse YAML config file
	configData, err := os.ReadFile(configPath)
	if err != nil {
		startupLogger.Error("Error reading config file", "path", configPath, "error", err)
		os.Exit(1)
	}

	var config Config
	err = yaml.Unmarshal(configData, &config)
	if err != nil {
		startupLogger.Error("Error parsing YAML config", "path", configPath, "error", err)
		os.Exit(1)
	}

	// Setup structured logging
	var fileLogger *slog.Logger
	var consoleLogger *slog.Logger

	// Setup file logger for warnings and errors
	if config.LogFile != "" {
		// Create log directory if it doesn't exist
		logDir := filepath.Dir(config.LogFile)
		err = os.MkdirAll(logDir, 0755)
		if err != nil {
			startupLogger.Error("Error creating log directory", "directory", logDir, "error", err)
			os.Exit(1)
		}

		// Open log file
		logFile, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			startupLogger.Error("Error opening log file", "path", config.LogFile, "error", err)
			os.Exit(1)
		}

		// Create file logger for Warn/Error levels
		fileLogger = slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
	} else {
		// If no log file specified, use stderr for errors
		fileLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
	}

	// Create console logger for Info/Debug levels (only when verbose)
	if verbose {
		consoleLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
		consoleLogger.Info("Reading config file", "path", configPath)
	}

	if verbose {
		consoleLogger.Info("Found targets to process", "count", len(config.Targets))
	}

	// Create retryable HTTP client with default settings
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 3 // Maximum number of retries
	if !verbose {
		retryClient.Logger = nil // Disable retry logging in non-verbose mode
	}

	// Process each target
	for i, target := range config.Targets {
		if verbose {
			consoleLogger.Info("Processing target",
				"index", i+1,
				"total", len(config.Targets),
				"name", target.Name,
				"url", target.URL)
		}

		// Make HTTP GET request with retry logic
		resp, err := retryClient.Get(target.URL)
		if err != nil {
			fileLogger.Error("Error fetching URL", "url", target.URL, "error", err)
			continue
		}

		// Check status code
		if resp.StatusCode != http.StatusOK {
			fileLogger.Error("Received non-OK status code",
				"url", target.URL,
				"status_code", resp.StatusCode)
			resp.Body.Close()
			continue
		}

		// Read response body
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fileLogger.Error("Error reading response body", "url", target.URL, "error", err)
			continue
		}

		if verbose {
			consoleLogger.Info("Successfully fetched data", "bytes", len(bodyBytes))
		}

		// Format JSON if requested and file extension is .json
		var dataToWrite []byte
		if strings.HasSuffix(strings.ToLower(target.Path), ".json") && jsonFormat != "original" {
			// Try to parse as JSON
			var jsonData interface{}
			if err := json.Unmarshal(bodyBytes, &jsonData); err != nil {
				fileLogger.Warn("Could not parse JSON, writing original content", "path", target.Path, "error", err)
				dataToWrite = bodyBytes
			} else {
				// Format the JSON based on the flag
				switch jsonFormat {
				case "pretty":
					formattedBytes, err := json.MarshalIndent(jsonData, "", "  ")
					if err != nil {
						fileLogger.Warn("Could not format JSON, writing original content", "path", target.Path, "error", err)
						dataToWrite = bodyBytes
					} else {
						dataToWrite = formattedBytes
						if verbose {
							consoleLogger.Info("Formatted JSON as pretty-printed")
						}
					}
				case "minimized":
					formattedBytes, err := json.Marshal(jsonData)
					if err != nil {
						fileLogger.Warn("Could not minimize JSON, writing original content", "path", target.Path, "error", err)
						dataToWrite = bodyBytes
					} else {
						dataToWrite = formattedBytes
						if verbose {
							consoleLogger.Info("Formatted JSON as minimized")
						}
					}
				case "both":
					// Write minimized version to original path
					minimizedBytes, err := json.Marshal(jsonData)
					if err != nil {
						fileLogger.Warn("Could not minimize JSON, writing original content", "path", target.Path, "error", err)
						dataToWrite = bodyBytes
					} else {
						dataToWrite = minimizedBytes
						if verbose {
							consoleLogger.Info("Formatted JSON as minimized")
						}
					}

					// Write pretty-printed version to .pp.json file
					prettyBytes, err := json.MarshalIndent(jsonData, "", "  ")
					if err != nil {
						fileLogger.Warn("Could not format pretty JSON", "path", target.Path, "error", err)
					} else {
						// Create pretty-printed filename by adding .pp before .json
						prettyPath := strings.TrimSuffix(target.Path, ".json") + ".pp.json"

						// Create directory for pretty file if needed
						prettyDir := filepath.Dir(prettyPath)
						err = os.MkdirAll(prettyDir, 0755)
						if err != nil {
							fileLogger.Error("Error creating directory", "directory", prettyDir, "error", err)
						} else {
							// Write pretty-printed file
							err = os.WriteFile(prettyPath, prettyBytes, 0644)
							if err != nil {
								fileLogger.Error("Error writing pretty JSON file", "path", prettyPath, "error", err)
							} else if verbose {
								consoleLogger.Info("Wrote pretty-printed version", "path", prettyPath)
							}
						}
					}
				}
			}
		} else {
			dataToWrite = bodyBytes
		}

		// Create directory if it doesn't exist
		dir := filepath.Dir(target.Path)
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			fileLogger.Error("Error creating directory", "directory", dir, "error", err)
			continue
		}

		// Write to file
		err = os.WriteFile(target.Path, dataToWrite, 0644)
		if err != nil {
			fileLogger.Error("Error writing to file", "path", target.Path, "error", err)
			continue
		}

		if verbose {
			consoleLogger.Info("Successfully wrote file", "path", target.Path)
		}
	}

	if verbose {
		consoleLogger.Info("Application finished successfully!")
	}
}
