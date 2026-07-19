.PHONY: all build test fmt vet clean \
        local local-down local-clean local-logs \
        prod prod-down prod-logs prod-token \
        deploy-validate run-local stop-local

all: build

# ---------------------------------------------------------------------------
# Dev toolchain
# ---------------------------------------------------------------------------
build:
	CGO_ENABLED=0 go build ./...

test:
	CGO_ENABLED=0 go test ./pkg/...

fmt:
	gofmt -w cmd/ pkg/

vet:
	CGO_ENABLED=0 go vet ./...

clean:
	rm -f kiwi kiwid kiwidaemon

# ---------------------------------------------------------------------------
# Local — one command. Control Plane in Docker + a Data Plane daemon on the
# host. Provider keys in deploy/.env are seeded so tasks run immediately.
# ---------------------------------------------------------------------------
local:
	@./scripts/local-up.sh

local-down:
	@./scripts/local-down.sh

local-clean:
	@./scripts/local-down.sh --clean

local-logs:
	@tail -f .kiwi-local/daemon.log

# ---------------------------------------------------------------------------
# Prod — one command. Full stack (Postgres + Control Plane + Caddy TLS +
# containerized daemon). Requires a filled deploy/.env; see deploy/README.md.
# ---------------------------------------------------------------------------
prod:
	@./scripts/prod-up.sh

prod-down:
	docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env down

prod-logs:
	docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env logs -f kiwid kiwidaemon

# Validate the prod compose file parses.
deploy-validate:
	docker compose -f deploy/docker-compose.prod.yml config -q

# ---------------------------------------------------------------------------
# Legacy v0 dev-dependency stack (Postgres + NATS + MinIO). Not the BYOC app
# stack — use `make local` for that.
# ---------------------------------------------------------------------------
run-local:
	docker compose up --build -d

stop-local:
	docker compose down -v
