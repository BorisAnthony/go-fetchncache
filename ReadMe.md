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
```

`headers` are optional.

See more example YAML files in `./config`.


## Usage

`-v`: verbose mode (_`debug` & `info` console logging_)

`--config`: (required) path to config 

`--json-format`: one of `original`, `pretty`, `minimized` or `both`
- if called without, will default to `original`
- `both` will generate both `pretty` and `minimized`, adding a "-pp" suffix to the pp'ed one.

_**Example**_

`./fetchncache --config ./config/example.yaml -v --json-format both`