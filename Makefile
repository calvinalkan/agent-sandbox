SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
MAKEFLAGS += --warn-undefined-variables --no-builtin-rules -j
.SUFFIXES:
.DELETE_ON_ERROR:
.DEFAULT_GOAL := build

.PHONY: build test lint clean install vet install-tools check fmt modernize

BINARY := agent-sandbox
GO := go
VERSION := dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")$(shell git diff --quiet 2>/dev/null || echo "-dirty")
DATE := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agent-sandbox
	@[ -e ~/.local/bin/$(BINARY) ] || ln -sf $(CURDIR)/$(BINARY) ~/.local/bin/$(BINARY)

vet:
	$(GO) vet ./...

fmt:
	golangci-lint run --fix --enable-only=modernize
	golangci-lint fmt

modernize:
	modernize -fix ./...

lint:
	golangci-lint config verify
	@for script in ./backpressure/*.sh; do "$$script"; done
	golangci-lint run --fix ./...

test:
	$(GO) test -race ./...

clean:
	rm -f $(BINARY)

install:
	$(GO) install ./cmd/agent-sandbox

install-tools:
	@echo "golangci-lint includes all needed tools (modernize, etc.)"
	@echo "Install with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"

check: vet lint test
