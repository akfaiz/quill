GO       ?= go
FUZZTIME ?= 20s
INTEGRATION_DIR := test/integration

.DEFAULT_GOAL := help
.PHONY: help build fmt vet lint test test-integration test-integration-lite \
        test-postgres test-mysql test-mariadb test-sqlite fuzz tidy check ci

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the root module
	$(GO) build ./...

fmt: ## Format code with gofmt
	gofmt -w .

vet: ## Run go vet on both modules
	$(GO) vet ./...
	cd $(INTEGRATION_DIR) && $(GO) vet ./...

lint: ## Run golangci-lint on both modules
	golangci-lint run ./...
	cd $(INTEGRATION_DIR) && golangci-lint run ./...

test: ## Unit and golden tests (root module, race detector on)
	$(GO) test -race ./...

test-integration: ## End-to-end tests on all databases (needs Docker)
	cd $(INTEGRATION_DIR) && $(GO) test -race ./...

test-integration-lite: ## End-to-end tests on SQLite only (no Docker)
	cd $(INTEGRATION_DIR) && GOQUERY_SKIP_CONTAINERS=1 $(GO) test -race ./...

test-postgres: ## Integration tests, PostgreSQL only (needs Docker)
	cd $(INTEGRATION_DIR) && $(GO) test -race -run TestPostgres ./...

test-mysql: ## Integration tests, MySQL only (needs Docker)
	cd $(INTEGRATION_DIR) && $(GO) test -race -run TestMySQL ./...

test-mariadb: ## Integration tests, MariaDB only (needs Docker)
	cd $(INTEGRATION_DIR) && $(GO) test -race -run TestMariaDB ./...

test-sqlite: ## Integration tests, SQLite only (no Docker)
	cd $(INTEGRATION_DIR) && $(GO) test -race -run TestSQLite ./...

fuzz: ## Run every fuzz target for FUZZTIME (default 20s)
	$(GO) test -run=NONE -fuzz=FuzzIdentifierQuoting          -fuzztime=$(FUZZTIME) ./db
	$(GO) test -run=NONE -fuzz=FuzzRawPlaceholders            -fuzztime=$(FUZZTIME) ./db
	$(GO) test -run=NONE -fuzz=FuzzQuoteCannotBeEscaped       -fuzztime=$(FUZZTIME) ./internal/dialect
	$(GO) test -run=NONE -fuzz=FuzzPathCannotEscapeItsLiteral -fuzztime=$(FUZZTIME) ./internal/jsonpath
	$(GO) test -run=NONE -fuzz=FuzzPgAttributesQuoting        -fuzztime=$(FUZZTIME) ./internal/jsonpath
	$(GO) test -run=NONE -fuzz=FuzzInterpolateNeverPanics     -fuzztime=$(FUZZTIME) ./internal/rawsql
	$(GO) test -run=NONE -fuzz=FuzzDecode                     -fuzztime=$(FUZZTIME) ./internal/keyset

tidy: ## go mod tidy both modules without letting a newer toolchain bump the go line
	GOTOOLCHAIN=local $(GO) mod tidy
	cd $(INTEGRATION_DIR) && GOTOOLCHAIN=local $(GO) mod tidy

check: build vet lint test ## Fast pre-commit gate (no Docker)

ci: check test-integration ## Full gate, including Docker-backed integration tests
