#!/usr/bin/env bash
#
# One-command local Kiwi: Control Plane in Docker + a Data Plane daemon on the
# host. The daemon runs on the host (USE_DOCKER=false) so it uses your local
# Go/git toolchain directly — no Docker-in-Docker, which is the reliable path on
# a dev machine. Provider keys present in deploy/.env are seeded automatically so
# the daemon can run real tasks the moment it registers.
#
# Usage: make local   (or: scripts/local-up.sh)
set -euo pipefail
cd "$(dirname "$0")/.."

ENV_FILE=deploy/.env
COMPOSE="docker compose -f deploy/docker-compose.prod.yml -f deploy/docker-compose.local.yml --env-file $ENV_FILE"
API=http://localhost:8080
# Absolute so the daemon's -cache-dir/-key-path resolve the same no matter what
# cwd the loop, git cache, and sandbox each run from.
STATE_DIR="$(pwd)/.kiwi-local"

# 1. Ensure an env file exists — generate fresh secrets on first run so this
#    works from a clean clone with nothing configured.
if [ ! -f "$ENV_FILE" ]; then
  echo "→ generating $ENV_FILE with fresh secrets"
  cat > "$ENV_FILE" <<EOF
POSTGRES_USER=kiwi
POSTGRES_PASSWORD=kiwipassword
POSTGRES_DB=kiwi
KIWI_ENCRYPTION_KEY=$(openssl rand -hex 32)
KIWI_SERVER_TOKEN=$(openssl rand -hex 32)
KIWI_CORS_ALLOWED_ORIGINS=http://localhost:3000
DOMAIN=localhost
# Optional — fill these so the local daemon can run real tasks:
ANTHROPIC_API_KEY=
GEMINI_API_KEY=
GITHUB_TOKEN=
EOF
fi
set -a; . "$ENV_FILE"; set +a
: "${KIWI_SERVER_TOKEN:?KIWI_SERVER_TOKEN must be set in $ENV_FILE}"

# 2. Control Plane (Postgres + kiwid), built and healthy.
echo "→ starting Control Plane (postgres, kiwid)"
$COMPOSE up -d --build postgres kiwid

echo -n "→ waiting for kiwid to be ready"
for i in $(seq 1 60); do
  if curl -sf -o /dev/null "$API/"; then echo " ✓"; break; fi
  echo -n "."; sleep 1
  if [ "$i" = 60 ]; then echo " timed out"; exit 1; fi
done

# 3. Seed provider credentials that are present in the env (org "system" is the
#    KIWI_SERVER_TOKEN identity). Missing keys are skipped — not an error.
seed_cred() { # name kind value
  [ -z "${3:-}" ] && return 0
  if curl -sf -o /dev/null -X POST "$API/api/v1/credentials" \
       -H "Authorization: Bearer $KIWI_SERVER_TOKEN" -H "Content-Type: application/json" \
       -d "{\"name\":\"$1\",\"kind\":\"$2\",\"value\":\"$3\"}"; then
    echo "→ seeded $1"
  else
    echo "! failed to seed $1"
  fi
}
seed_cred ANTHROPIC_API_KEY llm "${ANTHROPIC_API_KEY:-}"
seed_cred GEMINI_API_KEY    llm "${GEMINI_API_KEY:-}"
seed_cred GIT_TOKEN         git "${GITHUB_TOKEN:-}"

# 4. Mint a single-use join token and (re)start the host daemon.
mkdir -p "$STATE_DIR"
JOIN=$(curl -sf -X POST "$API/api/v1/daemon/join-token" \
  -H "Authorization: Bearer $KIWI_SERVER_TOKEN" -H "Content-Type: application/json" -d '{}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["join_token"])')

echo "→ building host daemon"
if [ "$(uname)" = "Darwin" ]; then
  # macOS dyld requires external linking + an ad-hoc signature (see CLAUDE.md §4).
  go build -ldflags="-linkmode=external" -o "$STATE_DIR/kiwidaemon" ./cmd/kiwidaemon
  codesign -s - -f "$STATE_DIR/kiwidaemon" >/dev/null 2>&1 || true
else
  go build -o "$STATE_DIR/kiwidaemon" ./cmd/kiwidaemon
fi

# Stop a daemon left over from a previous `make local`.
if [ -f "$STATE_DIR/daemon.pid" ]; then
  kill "$(cat "$STATE_DIR/daemon.pid")" 2>/dev/null || true
fi

echo "→ starting host daemon (USE_DOCKER=false)"
USE_DOCKER=false KIWI_JOIN_TOKEN="$JOIN" nohup "$STATE_DIR/kiwidaemon" \
  -api-url "$API" \
  -key-path "$STATE_DIR/daemon.key" \
  -cache-dir "$STATE_DIR/cache" \
  -poll-interval 3s \
  > "$STATE_DIR/daemon.log" 2>&1 &
echo $! > "$STATE_DIR/daemon.pid"
sleep 3

echo ""
echo "✅ Kiwi is up."
echo "   Control Plane : $API"
echo "   Daemon        : host process (pid $(cat "$STATE_DIR/daemon.pid")), log: $STATE_DIR/daemon.log"
echo "   Admin token   : KIWI_SERVER_TOKEN in $ENV_FILE"
echo "   Frontend      : cd frontend && NEXT_PUBLIC_KIWI_API_URL=$API npm run dev"
echo "   Stop          : make local-down"
