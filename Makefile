.PHONY: build test test-verbose test-coverage test-python test-typescript test-all \
       lint fmt clean validate docker docker-up docker-down \
       gui-dev gui-build \
       helm-lint helm-template helm-package \
       release run setup help

BINARY    := mockagents
GO        := go
VERSION   ?= dev
LDFLAGS   := -s -w -X main.version=$(VERSION)

## Build
build:                          ## Compile Go binary
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/mockagents/

## Testing
test:                           ## Run Go tests
	$(GO) test ./... -count=1 -timeout 5m

test-verbose:                   ## Run Go tests with verbose output
	$(GO) test ./... -v -count=1 -timeout 5m

test-race:                      ## Run Go tests with race detector
	$(GO) test ./... -count=1 -race -timeout 5m

test-coverage:                  ## Run Go tests with coverage report
	$(GO) test ./... -coverprofile=coverage.out -count=1 -timeout 5m
	$(GO) tool cover -func=coverage.out
	@echo "---"
	@$(GO) tool cover -func=coverage.out | grep total

test-python:                    ## Run Python SDK tests
	cd sdk/python && python -m pytest tests/ -v

test-typescript:                ## Run TypeScript SDK tests
	cd sdk/typescript && npm test

test-all: test test-python test-typescript ## Run all tests (Go + Python + TypeScript)

## Code Quality
lint:                           ## Run Go vet
	$(GO) vet ./...

fmt:                            ## Format Go code
	gofmt -w .

## Validation
validate: build                 ## Validate example agent definitions
	./$(BINARY) validate examples/

## GUI
gui-dev:                        ## Run the web console in dev mode on :3001
	cd gui && npm run dev

gui-build:                      ## Build the web console for production
	cd gui && npm run build

## Helm
helm-lint:                      ## Lint the MockAgents Helm chart
	helm lint ./deploy/helm/mockagents --values ./deploy/helm/mockagents/ci/test-values.yaml

helm-template:                  ## Render the chart with CI test values
	helm template demo ./deploy/helm/mockagents --values ./deploy/helm/mockagents/ci/test-values.yaml

helm-package:                   ## Package the chart into dist/*.tgz
	mkdir -p dist
	helm package ./deploy/helm/mockagents --destination dist

## Docker
docker:                         ## Build Docker image
	docker build -t $(BINARY):latest .

docker-up:                      ## Start services with docker-compose
	docker compose up -d

docker-down:                    ## Stop docker-compose services
	docker compose down

## Release
release:                        ## Build release binaries with GoReleaser (dry run)
	goreleaser release --snapshot --clean

## Development
run: build                      ## Build and run with example agents
	./$(BINARY) start --agents-dir examples --log-level debug

setup:                          ## Install development tools
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	@echo "Go tools installed."
	@if command -v pip > /dev/null 2>&1; then \
		cd sdk/python && pip install -e ".[dev]"; \
		echo "Python SDK installed."; \
	fi

## Cleanup
clean:                          ## Remove build artifacts
	rm -f $(BINARY) $(BINARY).exe coverage.out .mockagents.db

## Help
help:                           ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
