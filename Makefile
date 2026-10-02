BIN     := bin/lazyforge
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test lint fmt fmt-check check run clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/lazyforge

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

fmt-check:
	golangci-lint fmt --diff

check: fmt-check lint test build

run: build
	./$(BIN)

clean:
	rm -rf bin
