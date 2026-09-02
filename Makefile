GO      ?= go
BIN     ?= bin/phenk
PKGS    := ./...

.PHONY: help
help:
	@echo "make preflight   run CI's own steps locally (the only gate that matters)"
	@echo "make generate    regenerate API stubs from api/openapi.yaml"
	@echo "make build       build the phenk binary"
	@echo "make test        run the Go test suite"
	@echo "make fmt         gofmt the tree"
	@echo "make dev-db      start the development database"
	@echo "make migrate     apply migrations to \$$PHENK_DATABASE_URL"

# preflight runs the steps out of .github/workflows/ci.yml, so there is no
# second copy of the build to drift from.
.PHONY: preflight
preflight:
	@python3 scripts/preflight.py

# Regenerates the API server stubs from api/openapi.yaml. CI fails if the
# checked-in output is stale, so the implementation cannot drift from the
# published contract.
.PHONY: generate
generate:
	$(GO) generate ./...

.PHONY: build
build:
	$(GO) build -o $(BIN) ./cmd/phenk

.PHONY: test
test:
	$(GO) test -race -count=1 $(PKGS)

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: vet
vet:
	$(GO) vet $(PKGS)

.PHONY: dev-db
dev-db:
	docker compose up -d postgres

.PHONY: migrate
migrate:
	$(GO) run ./cmd/phenk migrate

.PHONY: clean
clean:
	rm -rf bin
