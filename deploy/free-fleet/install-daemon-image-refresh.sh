#!/bin/sh
# Install the daemon-image refresh timer on a free-fleet host.
#
# This existed only as files on one machine for its whole life: the script in
# /usr/local/bin and two units in /etc/systemd/system, in no repository. That is
# the same failure the provisioner had before install-provisioner.sh — the only
# record of how the host worked was the host — and it means a rebuilt or second
# fleet host silently serves a stale daemon image with nothing to say so.
#
# Idempotent: re-run it to pick up a change to the script or the units.
set -e
HERE=$(cd "$(dirname "$0")" && pwd)

install -m 0755 "$HERE/kiwi-refresh-daemon-image.sh" /usr/local/bin/kiwi-refresh-daemon-image.sh
install -m 0644 "$HERE/kiwi-daemon-image.service" /etc/systemd/system/kiwi-daemon-image.service
install -m 0644 "$HERE/kiwi-daemon-image.timer"   /etc/systemd/system/kiwi-daemon-image.timer

systemctl daemon-reload
systemctl enable --now kiwi-daemon-image.timer

echo "installed. next run:"
systemctl list-timers kiwi-daemon-image.timer --no-pager | sed -n 2p
