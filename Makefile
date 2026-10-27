# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (c) 2026 The GotCode Collective
# See the LICENSE file in the project root for full license details.

SHELL := /usr/bin/env bash

# Build metadata
GIT_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-dev")
GIT_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
	-X "gotcode.org/crucible/internal/version.gitVersion=$(GIT_VERSION)" \
	-X "gotcode.org/crucible/internal/version.gitCommit=$(GIT_COMMIT)" \
	-X "gotcode.org/crucible/internal/version.buildDate=$(BUILD_DATE)"

.PHONY: all build check lint sec vuln test clean tidy

all: tidy lint sec vuln test build

build: tidy
	@echo "==> Building crucibled and cruciblectl (CGO_ENABLED=0)..."
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o bin/crucibled ./cmd/crucibled
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o bin/cruciblectl ./cmd/cruciblectl

check: tidy sec vuln test

lint:
	@echo "==> Running golangci-lint..."
	golangci-lint run ./...

sec:
	@echo "==> Running gosec security analysis..."
	go run github.com/securego/gosec/v2/cmd/gosec@v2.22.2 -quiet ./...

vuln:
	@echo "==> Running govulncheck vulnerability scanner..."
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

test:
	@echo "==> Running tests with race detection..."
	CGO_ENABLED=0 go test -v ./...
	go test -v -race ./...

clean:
	@echo "==> Cleaning build artifacts..."
	@rm -rf bin/ dist/ *.test *.out cover.out coverage.html

tidy:
	@echo "==> Tidying and verifying Go module dependencies..."
	go mod tidy
	go mod download
