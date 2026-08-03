#!/bin/sh
# Keep the free-tier daemon image current on this host.
#
# The provisioner launches per-org daemons from a LOCAL tag and never pulls:
# Docker resolves registry credentials client-side, inside the provisioner
# container, and that container is deliberately cut off from the metadata
# endpoint by harden-egress.sh. Pulling here instead keeps registry credentials
# on the host, where they belong, and leaves the container needing no registry
# access at all.
#
# Without this the host silently serves whatever image it last cached — which is
# exactly how it ended up ten days stale.
set -e
REMOTE=us-central1-docker.pkg.dev/kiwi-prod-502913/kiwi-repo/kiwidaemon:latest
LOCAL=kiwidaemon:latest

docker pull -q "$REMOTE"
docker tag "$REMOTE" "$LOCAL"
echo "refreshed $LOCAL from $REMOTE ($(docker inspect --format "{{.Id}}" "$LOCAL"))"

# Retagging LOCAL to the new image leaves the previous one untagged, so every
# refresh orphans a whole daemon image (~170MB, more when a base layer changes).
# They are invisible to `docker images` and accumulate silently: the host
# reached 98% and could not pull ANY image — cold starts failed with "no space
# left on device", which surfaces as a broken free tier rather than as a disk
# alert. Twenty-four orphans had built up by then.
#
# `prune` without -a is the whole point. It removes only dangling (untagged)
# images. `-a` would additionally delete every tagged image not currently
# attached to a running container — including kiwidaemon:latest itself, which
# the provisioner resolves locally and cannot re-pull, and the sandbox base
# images (golang, node, postgres) that a task would then have to fetch again
# mid-run.
#
# Non-fatal: a refreshed image the provisioner can use matters more than
# reclaiming space, so a prune failure must not fail the unit and leave the
# host on a stale daemon.
if reclaimed=$(docker image prune -f 2>&1); then
  echo "$reclaimed" | grep -i "^Total reclaimed" || echo "pruned: nothing to reclaim"
else
  echo "warning: image prune failed; disk may fill" >&2
fi
