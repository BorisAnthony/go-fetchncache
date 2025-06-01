.PHONY: build-linux build-macos run clean test

build-linux:
	GOOS=linux GOARCH=amd64 go build -o ./dist/linux-amd64/fetchncache main.go

build-macos:
	GOOS=darwin GOARCH=arm64 go build -o ./dist/darwin-arm64/fetchncache main.go

run:
	go run main.go --config ./config/example.yaml --json-format both -v

clean:
	rm -rf dist/

test:
	go test ./...

install-deps:
	go mod download
	go mod tidy