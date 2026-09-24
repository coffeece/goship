.PHONY: build install test lint vuln check clean tidy

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/goship ./cmd/goship

install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/goship

test:
	go test -race ./...

lint:
	golangci-lint run

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# check is what CI runs.
check: lint test vuln

clean:
	rm -rf bin/ dist/

tidy:
	go mod tidy
