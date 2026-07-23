#!/usr/bin/env bash
#
# Harden egress on the Kiwi free-fleet host.
#
# The free fleet packs many orgs' daemon/sandbox containers onto one host. This
# blocks those containers from reaching the cloud metadata endpoint — which would
# otherwise hand out the VM's service-account token (an SSRF -> full-fleet
# compromise) — while leaving intact the egress the daemon legitimately needs
# (the Control Plane and LLM APIs, both public).
#
# Critical nuance: on GCP the metadata IP (169.254.169.254) is ALSO the DNS
# resolver. Blocking the whole IP breaks DNS and takes the fleet offline. So we
# block only the metadata HTTP ports (:80/:443) and leave :53 (DNS) open.
#
# Idempotent — safe to re-run. Rules live in Docker's DOCKER-USER chain, which
# nftables/iptables evaluates before Docker's own rules for every container's
# forwarded traffic.
#
# Usage:  sudo -E ./harden-egress.sh
#         sudo -E BLOCK_PRIVATE=1 ./harden-egress.sh   # also block RFC1918 egress
set -euo pipefail

CHAIN="DOCKER-USER"
META="169.254.169.254"
BLOCK_PRIVATE="${BLOCK_PRIVATE:-0}"   # 1 also blocks 10/8, 172.16/12, 192.168/16

[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo -E $0)"; exit 1; }
command -v iptables >/dev/null 2>&1 || { echo "iptables not found"; exit 1; }

# Docker creates DOCKER-USER; create it ourselves if Docker isn't up yet so the
# rules are already in place when it starts.
iptables -L "$CHAIN" -n >/dev/null 2>&1 || iptables -N "$CHAIN"

ins() { # ins <match args...> : insert a rule once at the top of DOCKER-USER
  if iptables -C "$CHAIN" "$@" 2>/dev/null; then
    echo "  = $* (already set)"
  else
    iptables -I "$CHAIN" 1 "$@"
    echo "  + $*"
  fi
}

echo "Hardening $CHAIN egress:"

# 1) Metadata token endpoint (HTTP). This is the crown-jewel vector: a container
#    that reaches it can read the VM's service-account token. DNS (:53 on the
#    same IP) is deliberately left open.
ins -d "$META" -p tcp --dport 80 -j DROP
ins -d "$META" -p tcp --dport 443 -j DROP

# 2) Optional: private-range egress (cross-tenant / host-service lateral moves).
#    Off by default because a daemon that must reach a VPC-internal service would
#    break. The free-fleet daemon only needs the public CP + LLM APIs, so turning
#    this on is safe there.
if [ "$BLOCK_PRIVATE" = "1" ]; then
  for net in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16; do
    ins -d "$net" -j DROP
  done
fi

echo "Done. DNS (:53 to $META) and public internet egress remain open."
echo
echo "Next:"
echo "  * Verify:   ./verify-egress.sh"
echo "  * Persist:  iptables rules are NOT kept across reboot — re-apply on boot"
echo "              (netfilter-persistent, cloud-init, or a systemd oneshot)."
echo "  * Tenant L2 isolation: also set {\"icc\": false} in /etc/docker/daemon.json"
echo "              so containers on the shared bridge can't talk to each other."
