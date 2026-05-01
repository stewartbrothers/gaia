SHELL := /usr/bin/env bash
GO    ?= go

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X 'github.com/stewartbrothers/gaia/internal/version.Version=$(VERSION)' \
           -X 'github.com/stewartbrothers/gaia/internal/version.Commit=$(COMMIT)'

.PHONY: all build build-gaia build-mcp test vet fmt lint tidy clean

all: build

build: build-gaia build-mcp

build-gaia:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/gaia ./cmd/gaia

build-mcp:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/gaia-mcp ./cmd/gaia-mcp

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -s -w .

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
	  echo "golangci-lint not found — see https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run ./...

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin/ dist/ coverage.out coverage.html
