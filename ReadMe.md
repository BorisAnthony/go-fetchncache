# Go FetchNCache

Fast 'n lightweight, does what it says.

Given a URL and a path, will fetch that URL and write the response to the given path.

My usecase was caching JSON responses from API so it assumes JSON, but it'll work with anything if you don't set the `--json-format` flag.


- Implements `hashicorp/go-retryablehttp` default retry & backoff strategy
- `log/slog` for **console** (_`debug` & `info` in verbose mode_) and **file** (_`warning` & `error` always_) logging


## Installation

Grab a [compiled binary from the releases](https://github.com/BorisAnthony/go-fetchncache/releases), decompress and place wherever you put your go binaries ( e.g.: `/usr/local/bin` ).

Make sure to give it executable permissions:
- `chmod +x /path/to/fetchncache`

On macOS, if you've downloaded the binary, you probably need to remove the quarantine:
- `xattr -d com.apple.quarantine /path/to/fetchncache`


## Compile

Tip: See the Makefile for more target options.

### macOS

(GOOS=darwin GOARCH=arm64)

`make build-macos`


### Linux

(GOOS=linux GOARCH=amd64)

`make build-linux`


## Config

Configuration is done via YAML files with the following structure:

```yaml
logfile: "./log/log.log"
targets:
  - name: "github-without-headers"
    url: "https://api.github.com/users/octocat"
    path: "./cache/github-no-headers.json"
  - name: "httpbin-with-headers" 
    url: "https://httpbin.org/headers"
    path: "./cache/httpbin-headers.json"
    headers:
      - "User-Agent: fetchncache-test/1.0" 
      - "X-Custom-Header: test-value"
      - "Accept: application/json"
  - name: "dated-archive-example"
    url: "https://api.github.com/users/github"
    path: 
      - string: "./cache/github-{pattern}.json"
        pattern: "DateTime-Asia/Tokyo-slug"
    headers:
      - "User-Agent: fetchncache/1.0"
  - name: "raw-timestamp-example"
    url: "https://api.github.com/users/github"
    path: 
      - string: "./cache/github-{pattern}.json"
        pattern: "RFC3339-UTC-none"
```

### Path Configuration

Paths can be configured in two ways:

#### Static Paths (Legacy)
```yaml
path: "./cache/data.json"
```

#### Dynamic Path Patterns (New)
```yaml
path: 
  - string: "./cache/data-{pattern}.json"
    pattern: "DateTime-Asia/Tokyo-slug"
```

**Pattern Format**: `DateTimeFormat-Timezone-Processing`

**Supported DateTime Formats** (Go time layout constants):
- **DateTime**: `2006-01-02 15:04:05` → `2025-01-28-15-30-45`
- **DateOnly**: `2006-01-02` → `2025-01-28`  
- **TimeOnly**: `15:04:05` → `15-30-45`
- **RFC3339**: `2006-01-02T15:04:05Z07:00` → `2025-01-28t15-30-45z07-00`
- **Kitchen**: `3:04PM` → `3-04pm`
- **Stamp**: `Jan _2 15:04:05` → `jan-2-15-04-05`

**Timezone Options**:
- **UTC**: Coordinated Universal Time (special case, always supported)
- **Any valid IANA timezone name**: e.g., `America/New_York`, `Europe/London`, `Asia/Tokyo`, `Australia/Sydney`
- **NOT supported**: 3-4 letter abbreviations (EST, CET, JST, PST, etc.) - use full IANA names instead

**Common Timezone Mappings**:
| ❌ Abbreviation | ✅ IANA Name | Description |
|---|---|---|
| EST/EDT | America/New_York | Eastern Time |
| CST/CDT | America/Chicago | Central Time |
| PST/PDT | America/Los_Angeles | Pacific Time |
| CET/CEST | Europe/Berlin | Central European Time |
| JST | Asia/Tokyo | Japan Standard Time |
| GMT/BST | Europe/London | British Time |  

**Processing Options**:
- **slug**: Filename-safe formatting using gosimple/slug library
  - Converts to lowercase for consistency
  - Replaces spaces, colons, and special characters with hyphens
  - Handles Unicode characters properly
  - Creates truly filename-safe strings
- **none**: No processing applied
  - Preserves original timestamp formatting
  - Maintains original case, spaces, and special characters
  - Use with caution on filesystems that don't support special characters

**Example outputs**:

*With slug processing:*
- `DateTime-Asia/Tokyo-slug` → `./cache/data-2025-01-28-15-30-45.json`
- `Kitchen-America/New_York-slug` → `./cache/data-8-23am.json`
- `RFC3339-Europe/London-slug` → `./cache/data-2025-08-14t13-23-28-01-00.json`

*With none processing:*
- `DateTime-UTC-none` → `./cache/data-2025-01-28 15:30:45.json`
- `Kitchen-America/New_York-none` → `./cache/data-8:23AM.json`
- `RFC3339-Europe/London-none` → `./cache/data-2025-08-14T13:23:28+01:00.json`

`headers` are optional.

See more example YAML files in `./config`.


## Usage

`-v`: verbose mode (_`debug` & `info` console logging_)

`--config`: (required) path to config 

`-d` / `--delay`: delay in seconds between targets (default: 0)
- Useful for rate limiting API requests
- Applies delay between all targets regardless of success/failure
- No delay after the final target

`--json-format`: one of `original`, `pretty`, `minimized` or `both`
- if called without, will default to `original`
- `both` will generate both `pretty` and `minimized`, adding a "-pp" suffix to the pp'ed one.
- works with both static paths and dynamic path patterns

`--latest`: create a "latest" copy of each downloaded file
- creates a copy with consistent filename: `data.json` → `latest.json`
- works with both static paths and dynamic path patterns
- with `--json-format both`, creates both `latest.json` and `latest.pp.json`
- useful for automation that needs predictable filenames

### Examples

#### Basic usage with static paths:
```bash
./fetchncache --config ./config/example.yaml -v --json-format both
```
Creates:
- `./cache/github-user.json` (minimized)
- `./cache/github-user.pp.json` (pretty-printed)

#### With delay between targets:
```bash
./fetchncache --config ./config/weather-test.yaml -v -d 5
```
Processes 6 weather API targets with 5-second delays between each request.

#### With --latest flag:
```bash
./fetchncache --config ./config/example.yaml --latest --json-format both -v
```
Creates:
- `./cache/github-user.json` (minimized main file)
- `./cache/github-user.pp.json` (pretty-printed main file)
- `./cache/latest.json` (minimized latest copy)
- `./cache/latest.pp.json` (pretty-printed latest copy)

#### With dynamic path patterns:
```bash
./fetchncache --config ./config/comprehensive-test.yaml -v --json-format both
```
Creates timestamped files like:
- `./cache/data-2025-01-28-15-30-45.json` (minimized)
- `./cache/data-2025-01-28-15-30-45.pp.json` (pretty-printed)

#### Dynamic paths with --latest flag:
```bash
./fetchncache --config ./config/comprehensive-test.yaml --latest -v
```
Creates both timestamped and latest files:
- `./cache/github-custom-agent-2025-01-28-15-30-45.json` (timestamped)
- `./cache/latest.json` (consistent latest copy)