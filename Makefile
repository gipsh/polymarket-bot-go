GO      ?= go
BINARY  := polymarket-bot
CMD     := ./cmd/bot

.PHONY: build run dry-run test tidy clean

build:
	$(GO) build -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

dry-run: build
	./$(BINARY) --dry-run

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BINARY)

# Cross-compile for Linux amd64
build-linux:
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BINARY)-linux $(CMD)
