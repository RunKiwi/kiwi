#!/usr/bin/env bash
#
# Tear down the local stack started by scripts/local-up.sh. Pass --clean to also
# drop the Postgres volume (wipes all data).
set -euo pipefail
cd "$(dirname "$0")/.."

STATE_DIR=.kiwi-local
COMPOSE="docker compose -f deploy/docker-compose.prod.yml -f deploy/docker-compose.local.yml --env-file deploy/.env"

if [ -f "$STATE_DIR/daemon.pid" ]; then
  if kill "$(cat "$STATE_DIR/daemon.pid")" 2>/dev/null; then
    echo "→ stopped host daemon"
  fi
  rm -f "$STATE_DIR/daemon.pid"
fi

if [ "${1:-}" = "--clean" ]; then
  $COMPOSE down -v
  rm -rf "$STATE_DIR"
  echo "✅ local stack stopped and data wiped"
else
  $COMPOSE down
  echo "✅ local stack stopped (Postgres volume kept; add --clean to wipe)"
fi
