# Makefile for building the Go CLI with version metadata injected via ldflags
SHELL := /bin/bash
GOPATH:=$(shell go env GOPATH)

# Binary name and output directory
BINARY_NAME ?= dino
BIN_DIR ?= bin

# Package path that holds the Version/GitCommit/BuildTime variables
PKG := dino/cmd

# Version metadata (fallbacks keep make from failing outside a git repo)
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo '')
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# golangci-lint version and paths
GOLANGCI_LINT_VERSION ?= v2.6.2
GOLANGCI_LINT_DIR ?= .tools/golangci-lint/$(GOLANGCI_LINT_VERSION)
GOLANGCI_LINT_BIN := $(GOLANGCI_LINT_DIR)/bin/golangci-lint

# ldflags to inject into the binary
LDFLAGS := -X $(PKG).Version=$(VERSION) -X $(PKG).GitCommit=$(GIT_COMMIT) -X $(PKG).BuildTime=$(BUILD_TIME)

.PHONY: init

init:
	@mkdir -p $(GOLANGCI_LINT_DIR)
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOLANGCI_LINT_DIR)/bin $(GOLANGCI_LINT_VERSION)

.PHONY: lint

lint:
	@echo "Running gofmt..."
	@gofmt -w -s .
	@if [ -x "$(GOLANGCI_LINT_BIN)" ]; then \
		$(GOLANGCI_LINT_BIN) run --fix ./...; \
	elif command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix ./...; \
	else \
		echo "golangci-lint not found. Run 'make init' to install $(GOLANGCI_LINT_VERSION), or install via: brew install golangci-lint or go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		echo "Running basic checks (go vet, gofmt) instead..."; \
		go vet ./...; \
		[ -z "$$(gofmt -s -l .)" ] || (echo "Files not gofmt'd:" && gofmt -s -l . && exit 1); \
	fi

.PHONY: build

build:
	@mkdir -p $(BIN_DIR)
	GO111MODULE=on go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) .
