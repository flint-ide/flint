WAILS   ?= $(shell go env GOPATH)/bin/wails
# The corporate Go proxy is only reachable on VPN; fall back to the public one.
export GOPROXY ?= https://proxy.golang.org,direct

PG_PORT ?= 55432
CH_PORT ?= 59000

.PHONY: dev build test test-go test-web test-integration test-db-up test-db-down install lint

dev: ## Run with hot reload (opens the app + http://localhost:34115)
	$(WAILS) dev -appargs "$(CURDIR)"

build: ## Build build/bin/flint.app
	$(WAILS) build -clean

test: test-go test-web ## Unit tests (Go + frontend)

test-go:
	go test ./internal/... -race -cover

test-web:
	cd frontend && npm run typecheck && npm test

test-db-up: ## Throwaway Postgres + ClickHouse for integration tests
	docker run -d --rm --name flint-test-pg -e POSTGRES_PASSWORD=flint-test -p $(PG_PORT):5432 postgres:16-alpine
	docker run -d --rm --name flint-test-ch -e CLICKHOUSE_USER=flint -e CLICKHOUSE_PASSWORD=flint-test \
		-p $(CH_PORT):9000 clickhouse/clickhouse-server:24.8-alpine

test-db-down:
	docker rm -f flint-test-pg flint-test-ch

test-integration: ## Requires `make test-db-up`
	FLINT_TEST_PG=localhost:$(PG_PORT) FLINT_TEST_CH=localhost:$(CH_PORT) \
		go test -tags integration -count=1 ./internal/db/

lint:
	go vet ./...
	gofmt -l . | (! grep .)

install: build ## Copy to /Applications and add a `flint` command to ~/.local/bin
	rm -rf /Applications/flint.app && cp -R build/bin/flint.app /Applications/
	mkdir -p $(HOME)/.local/bin
	install -m 755 scripts/flint $(HOME)/.local/bin/flint
