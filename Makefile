.PHONY: build install test lint clean tidy

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/goship ./cmd/goship

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/goship

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf bin/

tidy:
	go mod tidy
