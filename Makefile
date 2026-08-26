# vat — development commands.
#
# `make check` is the canonical proof that a change is complete. Nothing is
# merged without it passing.

BINARY      := vat
MODULE      := github.com/takealook97/vat
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --verify --quiet HEAD 2>/dev/null || echo unknown)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS     := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(BUILD_DATE)

# Measuring coverage without enforcing it is how a stated 80% becomes 60% one
# merge at a time. The figure was already within half a point of the line when
# this gate was added, so the next uncovered branch would have crossed it
# silently.
COVERAGE_MIN ?= 80

.DEFAULT_GOAL := check
.PHONY: check build install test cover lint fmt vet tidy clean release-snapshot help

## check: format, vet, test, and build — the canonical proof a change is complete
check: fmt vet test build

## build: compile the binary into ./bin
build:
	@mkdir -p bin
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/$(BINARY)
	@echo "built bin/$(BINARY) $(VERSION)"

## install: install the binary into GOBIN
install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/$(BINARY)

## test: run the full suite with the race detector
test:
	go test -race ./...

## cover: run tests and fail when coverage falls below COVERAGE_MIN
cover:
	go test -coverprofile=coverage.out ./...
	@total=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$NF}' | tr -d '%'); \
	echo "total coverage: $$total% (minimum $(COVERAGE_MIN)%)"; \
	awk -v have="$$total" -v want="$(COVERAGE_MIN)" \
		'BEGIN { exit !(have + 0 >= want + 0) }' || { \
		echo "coverage $$total% is below the $(COVERAGE_MIN)% minimum required by CONTRIBUTING.md"; \
		exit 1; }
	@echo "run 'go tool cover -html=coverage.out' for the annotated source"

## fmt: verify formatting; fails rather than rewriting, so CI and local agree
fmt:
	@unformatted=$$(gofmt -l . | grep -v '^$$' || true); \
	if [ -n "$$unformatted" ]; then \
		echo "these files are not gofmt-clean:"; echo "$$unformatted"; \
		echo "run: gofmt -w ."; exit 1; \
	fi

## vet: run go vet
vet:
	go vet ./...

## lint: run golangci-lint when it is installed
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint is not installed; skipping"; \
		echo "install: https://golangci-lint.run/welcome/install/"; \
	fi

## tidy: prune and verify module dependencies
tidy:
	go mod tidy
	go mod verify

## clean: discard build and coverage artefacts
clean:
	$(RM) -r bin dist coverage.out coverage.html

## release-snapshot: build binaries for every supported platform
release-snapshot:
	@mkdir -p dist
	@for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; ext=""; \
		[ "$$os" = windows ] && ext=".exe"; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/$(BINARY)_$${os}_$${arch}$$ext ./cmd/$(BINARY) || exit 1; \
	done
	@ls -la dist

## help: list the available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
