# Find the Gaps — developer shortcuts.

BIN := ftg
PKG := ./cmd/find-the-gaps
COVERAGE := coverage.out

.PHONY: help build test test-race cover cover-html lint fmt tidy clean all

help: ## Show this help.
	@awk 'BEGIN{FS=":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary into ./$(BIN).
	go build -o $(BIN) $(PKG)

test: ## Run unit + testscript tests.
	go test ./...

test-race: ## Run tests with the race detector.
	go test -race ./...

cover: ## Run tests with coverage summary.
	go test -cover ./...

cover-html: ## Generate HTML coverage report at coverage.html.
	go test -coverprofile=$(COVERAGE) ./...
	go tool cover -html=$(COVERAGE) -o coverage.html

lint: ## Run golangci-lint.
	golangci-lint run

fmt: ## Format Go sources with gofmt and goimports.
	gofmt -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || echo "(goimports not installed; skipping)"

tidy: ## Tidy go.mod / go.sum.
	go mod tidy

all: fmt tidy lint test ## Format, tidy, lint, test.

clean: ## Remove build artifacts.
	rm -f $(BIN) $(COVERAGE) coverage.html
