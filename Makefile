.PHONY: build test test-verbose test-coverage test-python test-typescript test-all \
       lint drift fmt clean validate docker docker-up docker-down \
       gui-dev gui-build \
       helm-lint helm-template helm-package \
       bench bench-report \
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

# The race detector requires CGO_ENABLED=1 and a C compiler (gcc/clang).
# This codebase is otherwise pure-Go (modernc.org/sqlite, no cgo), so a bare
# dev box — notably Windows without mingw — cannot run this target and will
# error with "requires cgo". CI runs it on the Linux and macOS legs, which
# always have a C toolchain; see CONTRIBUTING.md "Race detection".
test-race:                      ## Run Go tests with race detector (needs a C compiler)
	CGO_ENABLED=1 $(GO) test ./... -count=1 -race -timeout 5m

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

drift:                          ## Check api-spec $refs + license agreement (REF-06)
	$(GO) run ./tools/driftcheck

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

## Benchmarks
bench:                          ## Run Go benchmarks (engine hot paths)
	$(GO) test -run=^$$ -bench=. -benchmem -benchtime=1s ./internal/engine/...

bench-report:                   ## Run benchmarks and refresh docs/benchmarks/latest.{json,md}
	$(GO) run ./tools/benchreport -pkg ./internal/engine/... -out docs/benchmarks -benchtime 1s -count 1

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
