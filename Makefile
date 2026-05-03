SHELL := /usr/bin/env bash
GO    ?= go

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X 'github.com/stewartbrothers/gaia/internal/version.Version=$(VERSION)' \
           -X 'github.com/stewartbrothers/gaia/internal/version.Commit=$(COMMIT)'

COVER_PROFILE := coverage.out
COVER_HTML    := coverage.html

.PHONY: all build build-gaia build-mcp test test-race cover cover-html vet fmt lint tidy clean release-snapshot release-check dogfood-chain cache-bench

all: build

build: build-gaia build-mcp

build-gaia:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/gaia ./cmd/gaia

build-mcp:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/gaia-mcp ./cmd/gaia-mcp

test:
	$(GO) test ./...

test-race:
	$(GO) test ./... -race -count=1

cover:
	$(GO) test ./... -race -count=1 -covermode=atomic -coverprofile=$(COVER_PROFILE)
	$(GO) tool cover -func=$(COVER_PROFILE)

cover-html: cover
	$(GO) tool cover -html=$(COVER_PROFILE) -o $(COVER_HTML)
	@echo "→ open $(COVER_HTML)"

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

# goreleaser lives at $(go env GOPATH)/bin/goreleaser when installed
# via `go install` (the recommended path on this project — third-party
# brew packages have lagged behind v2). Targets prepend GOPATH/bin to
# PATH so they work whether or not the user has it on PATH already.
GOBIN := $(shell $(GO) env GOPATH)/bin

# Validate .goreleaser.yml without running a build. Cheap; useful in
# pre-commit hooks if a contributor edits the config.
release-check:
	@PATH="$(GOBIN):$$PATH" command -v goreleaser >/dev/null 2>&1 || { \
	  echo "goreleaser not found — install with: go install github.com/goreleaser/goreleaser/v2@v2.4.5"; exit 1; }
	@PATH="$(GOBIN):$$PATH" goreleaser check

# Local snapshot build — same goreleaser invocation the release
# workflow uses on tag push, but with `--snapshot` so the version
# string includes the commit SHA and `--skip=publish` so nothing is
# uploaded. Output lands in dist/. Run before tagging to confirm
# the archives look right.
release-snapshot:
	@PATH="$(GOBIN):$$PATH" command -v goreleaser >/dev/null 2>&1 || { \
	  echo "goreleaser not found — install with: go install github.com/goreleaser/goreleaser/v2@v2.4.5"; exit 1; }
	@PATH="$(GOBIN):$$PATH" goreleaser release --snapshot --clean --skip=publish

# Chain dogfood: prints the byte/token comparison of running the
# canned `pr-create-and-land` chain vs. the equivalent multi-call
# agent flow. Doesn't gate CI; the script does enforce a 50%
# reduction floor (override via DOGFOOD_THRESHOLD) so a regression
# trips loudly if anyone runs it locally. See
# bench/dogfood-chain.md for the per-step measurements and
# docs/chain-dogfood-comparison.md for the headline summary.
#
# Usage:
#   make dogfood-chain                                   # offline
#   PR_NUMBER=75 DOGFOOD_LIVE=1 make dogfood-chain       # against live forge
#   DOGFOOD_THRESHOLD=70 make dogfood-chain              # tighter gate
#
# Phase B-3 / #112.
dogfood-chain: build-gaia
	@./scripts/dogfood-chain.sh

# Cache benchmark — runs an issue-view loop with the SQLite read
# cache enabled vs. bypassed, prints latency + upstream-call counts.
# Default is offline (in-process httptest server); set CACHE_BENCH_LIVE=1
# to hit the configured forge instead. Phase 4 / #42.
cache-bench:
	@./scripts/cache-bench.sh
