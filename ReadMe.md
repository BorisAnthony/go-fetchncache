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
        pattern: "DateTime-JST-slug"
    headers:
      - "User-Agent: fetchncache/1.0"
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
    pattern: "DateTime-JST-slug"
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
- **JST**: Asia/Tokyo timezone
- **UTC**: Coordinated Universal Time  

**Processing Options**:
- **slug**: Lowercase, filename-safe formatting with timezone suffix

**Example outputs**:
- `DateTime-JST-slug` → `./cache/data-2025-01-28-15-30-45-jst.json`
- `DateOnly-UTC-slug` → `./cache/data-2025-01-28-utc.json`
- `Kitchen-JST-slug` → `./cache/data-3-04pm-jst.json`

`headers` are optional.

See more example YAML files in `./config`.


## Usage

`-v`: verbose mode (_`debug` & `info` console logging_)

`--config`: (required) path to config 

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
- `./cache/data-2025-01-28-15-30-45-jst.json` (minimized)
- `./cache/data-2025-01-28-15-30-45-jst.pp.json` (pretty-printed)

#### Dynamic paths with --latest flag:
```bash
./fetchncache --config ./config/comprehensive-test.yaml --latest -v
```
Creates both timestamped and latest files:
- `./cache/github-custom-agent-2025-01-28-15-30-45-jst.json` (timestamped)
- `./cache/latest.json` (consistent latest copy)