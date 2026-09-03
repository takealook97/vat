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
# CI builds and tests on three operating systems; `make check` runs on one. So
# `check` could pass on a change CI then rejected — the same hole the lint target
# below describes, one layer down. On 2026-08-27 it did: five tests failed only
# on Windows, one of them a real defect in the manifest lock.
#
# Vetting for each platform compiles every package for it, which catches an API
# that does not exist there and an import left unused under a build tag. It
# costs about three seconds and does not run the tests, so it cannot catch a
# behaviour that differs at run time. CI is still the only thing that can.
CI_PLATFORMS ?= linux darwin windows

COVERAGE_MIN ?= 80

# The total alone was hiding exactly what it was supposed to expose. At 80.5%
# overall, the three packages holding nearly all the logic — cli, lint, brain —
# were each below the stated line, floated there by small pure packages scoring
# in the nineties. A total is an average, and an average over unequal packages
# is not a floor.
#
# So each package carries its own minimum. It is deliberately set below where
# every package stands today: this is a ratchet against a package sliding, not a
# target to grind toward, and a gate that fails on the day it is added teaches
# people to pass -ldflags around it.
PACKAGE_COVERAGE_MIN ?= 75

.DEFAULT_GOAL := check
.PHONY: check build install test cover lint fmt vet tidy clean release-snapshot help

# `cover` is part of `check` for the reason the coverage comment above gives.
# The floor was enforced in CI and nowhere else, so the one gate written to stop
# coverage sliding could only report the slide after it had been pushed — and
# CONTRIBUTING.md lists coverage as a completion requirement, which made `check`
# not the proof it claims to be. It costs one more run of the suite; the race
# run cannot double as it, because a binary is either instrumented for coverage
# or for the race detector and `check` needs both answers.
## check: format, vet, lint, test, cover, and build — the canonical proof a change is complete
check: fmt vet lint test cover build

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

# One run of the suite, not two. `-coverprofile` already prints the per-package
# percentage that the second `go test -cover` was being run to collect, so that
# run was forty seconds spent re-deriving what the first one had printed and
# nobody had read. The output is buffered to a file rather than streamed because
# it has to be parsed; it is echoed as soon as the run ends, and on failure
# before anything else.
## cover: run tests and fail when total or per-package coverage falls too low
cover:
	@go test -coverprofile=coverage.out ./... > .coverage-packages 2>&1 || { \
		cat .coverage-packages; $(RM) .coverage-packages; exit 1; }
	@cat .coverage-packages
	@total=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$NF}' | tr -d '%'); \
	echo "total coverage: $$total% (minimum $(COVERAGE_MIN)%)"; \
	awk -v have="$$total" -v want="$(COVERAGE_MIN)" \
		'BEGIN { exit !(have + 0 >= want + 0) }' || { \
		echo "coverage $$total% is below the $(COVERAGE_MIN)% minimum required by CONTRIBUTING.md"; \
		$(RM) .coverage-packages; exit 1; }
	@awk -v want="$(PACKAGE_COVERAGE_MIN)" '\
		$$1 == "ok" { \
			for (i = 3; i <= NF; i++) if ($$i == "coverage:") pct = $$(i + 1) + 0; \
			if (pct < want) printf "  %s %.1f%%\n", $$2, pct; \
		}' .coverage-packages > .coverage-shortfall; \
	awk '$$1 != "ok" && $$1 != "?" && $$2 == "coverage:" { printf "  %s (no test files)\n", $$1 }' .coverage-packages > .coverage-untested; \
	if [ -s .coverage-untested ]; then \
		echo "not measured, so not gated:"; cat .coverage-untested; \
	fi; \
	if [ -s .coverage-shortfall ]; then \
		echo "below the $(PACKAGE_COVERAGE_MIN)% per-package floor:"; \
		cat .coverage-shortfall; \
		$(RM) .coverage-shortfall .coverage-untested .coverage-packages; exit 1; \
	fi; \
	$(RM) .coverage-shortfall .coverage-untested .coverage-packages; \
	echo "every measured package is at or above $(PACKAGE_COVERAGE_MIN)%"
	@echo "run 'go tool cover -html=coverage.out' for the annotated source"

## fmt: verify formatting; fails rather than rewriting, so CI and local agree
fmt:
	@unformatted=$$(gofmt -l . | grep -v '^$$' || true); \
	if [ -n "$$unformatted" ]; then \
		echo "these files are not gofmt-clean:"; echo "$$unformatted"; \
		echo "run: gofmt -w ."; exit 1; \
	fi

## vet: run go vet for this platform and for every platform CI builds on
vet:
	go vet ./...
	@for os in $(CI_PLATFORMS); do \
		printf 'go vet GOOS=%s\n' "$$os"; \
		GOOS=$$os go vet ./... || exit 1; \
	done

# Part of `check`. CI runs golangci-lint and `check` did not, so `check` could
# pass on a change CI then rejected — which makes it not the proof this file
# claims it is. It stays a soft dependency: a contributor without the linter
# gets a skip rather than a blocked build, and CI is still the backstop.
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
	$(RM) .coverage-packages .coverage-shortfall .coverage-untested

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
