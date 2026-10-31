WAILS   ?= $(shell go env GOPATH)/bin/wails3
export GOPROXY ?= https://proxy.golang.org,direct

PG_PORT ?= 55432
CH_PORT ?= 59000
CH_HTTP_PORT ?= 58123

.PHONY: dev build server docs vuln test test-go test-web test-integration test-db-up test-db-down install lint

dev: ## Run with live reload (Go rebuilds on change, the page reloads)
	$(WAILS) task dev

build: ## Build build/bin/flint.app
	$(WAILS) task package

server: ## Flint as an HTTP server for a browser, no window (http://localhost:8080)
	$(WAILS) task server -- "$(CURDIR)"

test: test-go test-web ## Unit tests (Go + frontend)

test-go:
	go test ./internal/... -race -cover

test-web:
	cd frontend && npm run typecheck && npm test

test-db-up: ## Throwaway Postgres + ClickHouse for integration tests
	docker run -d --rm --name flint-test-pg -e POSTGRES_PASSWORD=flint-test -p $(PG_PORT):5432 postgres:16-alpine
	docker run -d --rm --name flint-test-ch -e CLICKHOUSE_USER=flint -e CLICKHOUSE_PASSWORD=flint-test \
		-p $(CH_PORT):9000 -p $(CH_HTTP_PORT):8123 clickhouse/clickhouse-server:24.8-alpine

test-db-down:
	docker rm -f flint-test-pg flint-test-ch

test-integration: ## Requires `make test-db-up`
	FLINT_TEST_PG=localhost:$(PG_PORT) FLINT_TEST_CH=localhost:$(CH_PORT) FLINT_TEST_CH_HTTP=localhost:$(CH_HTTP_PORT) \
		go test -tags integration -count=1 ./internal/db/

lint:
	go vet ./...
	gofmt -l . | grep -v '^frontend/node_modules/' | (! grep .)

docs: ## Build the user docs into _site/ and preview them at http://localhost:8090
	go run ./cmd/docsite -serve :8090

vuln: ## Known vulnerabilities in Go and runtime npm dependencies
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	cd frontend && npm audit --omit=dev

install: build ## Copy to /Applications and add a `flint` command to ~/.local/bin
	rm -rf /Applications/flint.app && cp -R build/bin/flint.app /Applications/
	mkdir -p $(HOME)/.local/bin
	install -m 755 scripts/flint $(HOME)/.local/bin/flint
