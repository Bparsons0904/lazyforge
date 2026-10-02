BIN     := bin/lazyforge
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test lint fmt fmt-check vuln check run clean release install-test

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

install-test:
	shellcheck install.sh scripts/*.sh
	bash scripts/test-install.sh

check: fmt-check lint test vuln build install-test

run: build
	./$(BIN)

clean:
	rm -rf bin dist

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

release:
	rm -rf dist
	mkdir -p dist
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		stage=$$(mktemp -d); \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath \
			-ldflags "-s -w -X main.version=$(VERSION)" -o $$stage/lazyforge ./cmd/lazyforge || exit 1; \
		tar -C $$stage -czf dist/lazyforge_$(VERSION)_$${os}_$${arch}.tar.gz lazyforge || exit 1; \
		rm -rf $$stage; \
	done
	cd dist && (sha256sum *.tar.gz 2>/dev/null || shasum -a 256 *.tar.gz) > checksums.txt
