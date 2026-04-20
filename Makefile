.PHONY: all build test test-unit test-contract test-integration test-e2e lint vet fmt cover clean run help

GO              ?= go
GOLANGCI_LINT   ?= golangci-lint
PKG             := ./...
COVER_OUT       := coverage.out
COVER_THRESHOLD_DOMAIN := 85
COVER_THRESHOLD_APP    := 85

all: fmt vet lint test

build:
	$(GO) build -o bin/runtime-adapters ./cmd/runtime-adapters

test: test-unit test-contract

test-unit:
	$(GO) test -race -count=1 -coverprofile=$(COVER_OUT) $(PKG)

test-contract:
	$(GO) test -race -count=1 -tags=contract ./test/contract/...

test-integration:
	$(GO) test -race -count=1 -tags=integration -timeout=5m $(PKG)

test-e2e:
	$(GO) test -race -count=1 -tags=e2e -timeout=10m ./test/e2e/...

cover: test-unit
	$(GO) tool cover -func=$(COVER_OUT) | tail -n 1
	$(GO) tool cover -html=$(COVER_OUT) -o coverage.html

lint:
	$(GOLANGCI_LINT) run

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

run:
	$(GO) run ./cmd/runtime-adapters

clean:
	rm -rf bin/ coverage.out coverage.html

help:
	@echo "Targets: all build test test-unit test-contract test-integration test-e2e cover lint vet fmt run clean"
