.PHONY: build run test lint clean

build:
	go build -o bin/bot ./cmd/bot

run:
	go run ./cmd/bot

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

# Build a static binary for Linux (for server deploy)
build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/bot-linux ./cmd/bot
