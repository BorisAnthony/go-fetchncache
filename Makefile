VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS = -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build-linux build-macos build-windows run-dev test install-deps release-local

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o ./dist/linux-amd64/fetchncache main.go

build-macos:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o ./dist/darwin-arm64/fetchncache main.go

build-macos-intel:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o ./dist/darwin-amd64/fetchncache main.go

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o ./dist/windows-amd64/fetchncache.exe main.go

build-all: build-linux build-macos build-macos-intel build-windows

run-dev:
	go run main.go --config ./config/example.yaml --json-format both -v

test:
	go test ./...

install-deps:
	go mod download
	go mod tidy

# Build all platforms for release
release-local: clean build-all
	@echo "Built binaries for version: $(VERSION)"

clean:
	rm -rf ./dist