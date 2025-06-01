.PHONY: build-linux build-macos run-dev test

build-linux:
	GOOS=linux GOARCH=amd64 go build -o ./dist/linux-amd64/fetchncache main.go

build-macos:
	GOOS=darwin GOARCH=arm64 go build -o ./dist/darwin-arm64/fetchncache main.go

run-dev:
	go run main.go --config ./config/example.yaml --json-format both -v

test:
	go test ./...

install-deps:
	go mod download
	go mod tidy