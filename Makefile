# loradex CLI — build & dev tasks
BINARY      := loradex
PKG         := github.com/keeandrews/loradex-cli
BUILDINFO   := $(PKG)/internal/buildinfo
DIST        := dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(BUILDINFO).Version=$(VERSION) \
	-X $(BUILDINFO).Commit=$(COMMIT) \
	-X $(BUILDINFO).Date=$(DATE)

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the host binary
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/loradex

.PHONY: install
install: ## Install into GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/loradex

.PHONY: test
test: ## Run tests with race + coverage
	go test ./... -race -cover

.PHONY: lint
lint: ## go vet + gofmt check (golangci-lint if present)
	go vet ./...
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "(golangci-lint not installed; skipping)"

.PHONY: tidy
tidy: ## Tidy go.mod/go.sum
	go mod tidy

.PHONY: cross
cross: ## Cross-compile the five target platforms into dist/
	@mkdir -p $(DIST)
	@set -e; for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(DIST)/$(BINARY)-$$os-$$arch$$ext ./cmd/loradex; \
	done

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BINARY) $(DIST)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
