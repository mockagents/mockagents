# Agent Playground. The playground is part of the repository's Go module, so
# every target runs from the repository root.
ROOT := ../..
GO   ?= go
PKG  := ./demo/agent-playground

.PHONY: run run-auto run-external mock build test test-short verify mock-validate mock-test docker compose compose-external help

run:            ## Serve the playground with the embedded mock on :7070 (manual review)
	cd $(ROOT) && $(GO) run $(PKG)/cmd/playground serve

run-auto:       ## Serve with review_mode=auto (gates approve themselves)
	cd $(ROOT) && $(GO) run $(PKG)/cmd/playground serve --review-mode auto

run-external:   ## Serve against an external mockagents on :8080 (start it with `make mock`)
	cd $(ROOT) && $(GO) run $(PKG)/cmd/playground serve --mock http://127.0.0.1:8080

mock:           ## Start a standalone mockagents with the playground fixtures on :8080
	cd $(ROOT) && MOCKAGENTS_PRICING=$(PKG)/config/mock-pricing.yaml $(GO) run ./cmd/mockagents start --agents-dir $(PKG)/mockagents

build:          ## Build bin/playground
	cd $(ROOT) && $(GO) build -o demo/agent-playground/bin/playground $(PKG)/cmd/playground

test:           ## Unit, integration, contract and end-to-end tests
	cd $(ROOT) && $(GO) test $(PKG)/... -count=1 -timeout 10m

test-short:     ## Tests without the end-to-end verify
	cd $(ROOT) && $(GO) test $(PKG)/... -count=1 -short

verify:         ## 52-check end-to-end self-test against a private embedded instance
	cd $(ROOT) && $(GO) run $(PKG)/cmd/playground verify --embedded

mock-validate:  ## mockagents validate on the fixtures
	cd $(ROOT) && $(GO) run ./cmd/mockagents validate $(PKG)/mockagents

mock-test:      ## mockagents test: fixture contract suites
	cd $(ROOT) && $(GO) run ./cmd/mockagents test --agents-dir $(PKG)/mockagents $(PKG)/mock-tests/

docker:         ## Build the playground image
	cd $(ROOT) && docker build -f demo/agent-playground/Dockerfile -t mockagents-playground .

compose:        ## docker compose up (embedded mock)
	docker compose up --build playground

compose-external: ## docker compose with mockagents as its own container
	docker compose --profile external up --build mockagents playground-external

help:
	@grep -E '^[a-z-]+:.*##' Makefile | sed 's/:.*##/\t/'
