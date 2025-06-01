# Go FetchNCache

Fast 'n lightweight, does what it says.
- Implements `hashicorp/go-retryablehttp` default retry & backoff strategy
- `log/slog` for **console** (_`debug` & `info` in verbose mode_) and **file** (_`warning` & `error` always_) logging


## Compile


### macOS

(GOOS=darwin GOARCH=arm64)

`build-macos`


### Linux

(GOOS=linux GOARCH=amd64)

`build-linux`


## Config

See YAML file in `./config`


## Usage

`-v`: verbose mode (_`debug` & `info` console logging_)

`--config`: (required) path to config 

`--json-format`: one of `original`, `pretty`, `minimized` or `both`
- if called without, will default to `original`
- `both` will generate both `pretty` and `minimized`, adding a "-pp" suffix to the pp'ed one.

_**Example**_

`./fetchncache --config ./config/example.yaml -v --json-format both`