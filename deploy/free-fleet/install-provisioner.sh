#!/usr/bin/env bash
#
# Install (or repair) the free-tier provisioner on a fleet host.
#
# Idempotent: safe to re-run. It writes the systemd unit, creates the two
# config files if they are missing, and leaves existing config untouched — so
# re-running after a code change is just "install, restart".
#
# The provisioner needs exactly three things to work, and each has bitten us:
#
#   1. KIWI_PROVISIONER=docker      without it the poller never starts and the
#                                   container looks healthy while doing nothing
#   2. a reachable KIWI_DSN         it polls provisioning_requests directly
#   3. NO KIWI_DAEMON_IMAGE         see the note in provisioner.env below
#
# Usage:
#   sudo ./install-provisioner.sh                       # first install
#   sudo KIWI_DSN='host=... user=...' ./install-provisioner.sh
#   sudo IMAGE=<registry>/kiwid:latest ./install-provisioner.sh
set -euo pipefail

UNIT_SRC="$(dirname "$0")/kiwi-provisioner.service"
UNIT_DST=/etc/systemd/system/kiwi-provisioner.service
CONF_DIR=/etc/kiwi
ENV_FILE="$CONF_DIR/provisioner.env"
IMG_FILE="$CONF_DIR/provisioner.image"

[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo $0)"; exit 1; }
[ -f "$UNIT_SRC" ] || { echo "missing $UNIT_SRC"; exit 1; }

install -d -m 0755 "$CONF_DIR"

# --- image reference (not a secret; systemd reads this one) ---
if [ -n "${IMAGE:-}" ]; then
  echo "KIWI_PROVISIONER_IMAGE=$IMAGE" > "$IMG_FILE"
  chmod 0644 "$IMG_FILE"
  echo "wrote $IMG_FILE"
elif [ ! -f "$IMG_FILE" ]; then
  echo "KIWI_PROVISIONER_IMAGE=REPLACE_ME/kiwid:latest" > "$IMG_FILE"
  chmod 0644 "$IMG_FILE"
  echo "created $IMG_FILE — set the image, then re-run"
fi

# --- container environment (holds the DSN; host-only, never in git) ---
if [ ! -f "$ENV_FILE" ]; then
  cat > "$ENV_FILE" <<'EOF'
# Container environment for the Kiwi free-tier provisioner.
# docker --env-file parses this: KEY=value, one per line, NO quotes, and the
# value may contain spaces (the DSN does).

# Starts the provisioning poller. Gated separately from -role so a fleet host
# does not also run the singleton orchestrator sweepers. Without this the
# process starts, serves HTTP, and provisions nothing.
KIWI_PROVISIONER=docker

# Where cold-started daemons are told to call home. This is the PUBLIC Control
# Plane, not this host — the daemons poll it for leases.
KIWI_PUBLIC_API_URL=https://api.runkiwi.dev

# Same database the Control Plane uses; the poller reads provisioning_requests.
KIWI_DSN=REPLACE_ME

# Migrations are applied by the dedicated migrate job, never by a serving role.
KIWI_SKIP_BOOT_MIGRATE=true

# Deliberately NOT set: KIWI_DAEMON_IMAGE.
#
# Setting it to a registry reference turns on `docker run --pull=always` for
# every per-org daemon launch. Docker resolves registry credentials
# CLIENT-side — inside this container — and this container is cut off from the
# metadata endpoint by harden-egress.sh, so it cannot authenticate to Artifact
# Registry and every cold start fails to pull.
#
# Left unset, the launcher uses the local tag `kiwidaemon:latest`, which
# kiwi-daemon-image.timer refreshes on the HOST (where the credentials live).
# That is the whole design; do not "fix" it by setting this.
EOF
  chmod 0600 "$ENV_FILE"
  echo "created $ENV_FILE — set KIWI_DSN, then re-run"
fi

# Allow the DSN to be supplied on the command line for a scripted rebuild.
if [ -n "${KIWI_DSN:-}" ]; then
  sed -i "s|^KIWI_DSN=.*|KIWI_DSN=$KIWI_DSN|" "$ENV_FILE"
  echo "updated KIWI_DSN in $ENV_FILE"
fi

install -m 0644 "$UNIT_SRC" "$UNIT_DST"
systemctl daemon-reload
systemctl enable kiwi-provisioner.service

if grep -q REPLACE_ME "$ENV_FILE" "$IMG_FILE"; then
  echo
  echo "NOT started: $ENV_FILE / $IMG_FILE still contain REPLACE_ME."
  echo "Fill them in, then: systemctl restart kiwi-provisioner"
  exit 0
fi

systemctl restart kiwi-provisioner.service
sleep 3
systemctl --no-pager --lines=0 status kiwi-provisioner.service || true
echo
echo "Logs: journalctl -u kiwi-provisioner -f"
