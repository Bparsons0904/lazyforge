BIN     := bin/lazyforge
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test lint fmt fmt-check vuln check run clean

GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.8.0

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

vuln:
	go run $(GOVULNCHECK) ./...

check: fmt-check lint test vuln build

run: build
	./$(BIN)

clean:
	rm -rf bin
