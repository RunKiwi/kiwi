#!/usr/bin/env bash
#
# One-command production bring-up (single box): Postgres + Control Plane + Caddy
# (TLS) + a containerized Data Plane daemon. The daemon runs its task sandboxes
# in Docker (isolated), so it mounts the Docker socket.
#
# Requires a filled deploy/.env (KIWI_ENCRYPTION_KEY, KIWI_SERVER_TOKEN, DOMAIN,
# and provider keys). A join token is minted automatically and injected into the
# daemon; set KIWI_JOIN_TOKEN in deploy/.env to pin your own instead.
#
# Usage: make prod   (or: scripts/prod-up.sh)
set -euo pipefail
cd "$(dirname "$0")/.."

ENV_FILE=deploy/.env
[ -f "$ENV_FILE" ] || { echo "deploy/.env not found — copy the template and fill it (see deploy/README.md)"; exit 1; }
set -a; . "$ENV_FILE"; set +a
: "${KIWI_SERVER_TOKEN:?set KIWI_SERVER_TOKEN in deploy/.env}"
: "${KIWI_ENCRYPTION_KEY:?set KIWI_ENCRYPTION_KEY in deploy/.env}"

COMPOSE="docker compose -f deploy/docker-compose.prod.yml --env-file $ENV_FILE"

echo "→ starting Control Plane (postgres, kiwid, caddy)"
$COMPOSE up -d --build postgres kiwid caddy

# kiwid is internal (behind Caddy) in prod, so reach it over its own network
# namespace with a throwaway curl container rather than a published port.
KIWID_CID=$($COMPOSE ps -q kiwid)
oncp() { docker run --rm --network "container:$KIWID_CID" curlimages/curl:8.9.1 "$@"; }

echo -n "→ waiting for kiwid to be ready"
for i in $(seq 1 60); do
  if oncp -sf -o /dev/null http://localhost:8080/; then echo " ✓"; break; fi
  echo -n "."; sleep 1
  if [ "$i" = 60 ]; then echo " timed out"; exit 1; fi
done

# Mint a join token unless the operator pinned one in the env.
JOIN="${KIWI_JOIN_TOKEN:-}"
if [ -z "$JOIN" ]; then
  echo "→ minting a join token for the daemon"
  JOIN=$(oncp -sf -X POST http://localhost:8080/api/v1/daemon/join-token \
    -H "Authorization: Bearer $KIWI_SERVER_TOKEN" -H "Content-Type: application/json" -d '{}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["join_token"])')
fi

echo "→ starting Data Plane daemon"
KIWI_JOIN_TOKEN="$JOIN" $COMPOSE up -d --build kiwidaemon

echo ""
echo "✅ Kiwi (prod) is up."
echo "   Public URL : https://${DOMAIN:-<set DOMAIN in deploy/.env>}"
echo "   Services   : $($COMPOSE ps --services | tr '\n' ' ')"
echo "   Logs       : $COMPOSE logs -f kiwid kiwidaemon"
echo "   Stop       : make prod-down"
