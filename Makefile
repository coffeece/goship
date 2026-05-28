.PHONY: build install run test clean tidy

VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS    := -X main.version=$(VERSION) -X main.BuildCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/coffeece ./cmd/coffeece

# Installs to $GOBIN (or $GOPATH/bin, typically ~/go/bin) — make sure that
# directory is on your PATH.
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/coffeece

run: build
	./bin/coffeece

test:
	go test ./...

clean:
	rm -rf bin/

tidy:
	go mod tidy
