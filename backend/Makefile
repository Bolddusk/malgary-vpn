# Malgary backend developer targets.
GO ?= go

.PHONY: generate lint test test-integration build

generate: ## Regenerate sqlc queries and OpenAPI server code
	sqlc generate
	$(GO) tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	$(GO) tool oapi-codegen -config api/oapi-codegen-client.yaml api/openapi.yaml

lint:
	golangci-lint run ./...

test: ## Unit and contract tests (no containers)
	$(GO) test ./internal/... ./tests/contract/...

test-integration: ## Needs Docker: Postgres, Zitadel and NetBird containers
	$(GO) test -tags integration -timeout 20m ./tests/integration/...

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/malgaryd ./cmd/malgaryd
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/malgaryctl ./cmd/malgaryctl
