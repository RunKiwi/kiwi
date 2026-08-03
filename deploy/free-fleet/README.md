# Free-fleet host hardening

The **Free tier** packs many orgs' daemon and sandbox containers onto a shared
host. Two layers keep untrusted, model-generated code boxed in:

## Layer 1 — the sandbox has no network (already enforced)

The test command (the only place model-generated code runs) executes with
`--network none` (`pkg/sandbox`, set by the daemon at `pkg/daemon/daemon.go`).
So that code can't exfiltrate the repo, reach the LLM key, or touch the host.
This is guarded by a unit test (`TestBuildDockerArgs_NetworkNone`).

## Layer 2 — the host blocks the metadata endpoint (this directory)

The daemon container itself *does* have network (it must reach the Control Plane
and LLM APIs). The risk on a cloud VM is the **metadata endpoint**
`169.254.169.254`, which serves the VM's **service-account token** — an SSRF
from any container there would compromise the whole fleet. The comment in
`pkg/provisioner/docker.go` assumes "no ambient cloud creds"; these scripts are
what make that true.

```bash
sudo -E ./harden-egress.sh      # block the metadata token endpoint
./verify-egress.sh              # prove it: metadata blocked, internet still works
```

### What the rules do

- **Block** the metadata HTTP ports (`169.254.169.254:80` and `:443`) in Docker's
  `DOCKER-USER` chain — evaluated before Docker's own rules for all container
  traffic.
- **Keep DNS working.** On GCP the metadata IP is *also* the DNS resolver, so we
  block only the token ports, never `:53`. (Blocking the whole IP takes the fleet
  offline — a footgun this script exists to avoid.)
- **Leave public egress intact** so the daemon reaches the CP and LLM APIs.
- **Optional** `BLOCK_PRIVATE=1` also drops RFC1918 egress (cross-tenant / host
  lateral movement). Off by default so a VPC-internal dependency can't silently
  break; safe to enable for the public-only free-fleet daemon.

### Operational notes

- **Disk.** The host holds the OS, every sandbox base image, and a git cache per
  org, so it fills from three directions at once. It ran on a 10GB boot disk and
  hit 98%; it is now 50GB. Pruning slows the fill, it does not stop it — a fleet
  packing more orgs needs headroom, not just hygiene.

- **Persistence:** iptables rules don't survive a reboot. Re-apply on boot via
  `netfilter-persistent`, cloud-init, or a systemd oneshot that runs
  `harden-egress.sh`.
- **Tenant L2 isolation:** containers on the same Docker bridge can reach each
  other at layer 2 (that traffic never hits `DOCKER-USER`). Set
  `{"icc": false}` in `/etc/docker/daemon.json` to disable inter-container
  communication on the shared bridge.
- **Belt and braces:** where possible, also run the free-fleet VM with a
  minimal/no service account, so a metadata leak yields nothing useful.

### Demo

`verify-egress.sh` is the demoable proof for the security claim: it runs a probe
container, shows the service-account-token fetch failing, and shows a normal
HTTPS call succeeding — before and after `harden-egress.sh`.

---

## The provisioner service

The provisioner polls `provisioning_requests` and cold-starts a per-org daemon
container on submit. It runs under systemd:

```bash
sudo IMAGE=<registry>/kiwid:latest KIWI_DSN='host=… user=… dbname=kiwi sslmode=disable' \
  ./install-provisioner.sh

systemctl status kiwi-provisioner
journalctl -u kiwi-provisioner -f
```

`install-provisioner.sh` is idempotent — re-run it to pick up a unit change; it
will not overwrite an existing `/etc/kiwi/provisioner.env`.

| Path | Holds | Read by |
| --- | --- | --- |
| `/etc/systemd/system/kiwi-provisioner.service` | lifecycle | systemd |
| `/etc/kiwi/provisioner.image` | image reference | systemd |
| `/etc/kiwi/provisioner.env` (`0600`) | `KIWI_DSN`, `KIWI_PROVISIONER=docker`, `KIWI_PUBLIC_API_URL` | docker |

It exists as a unit for a specific reason: the provisioner used to be a
hand-issued `docker run`, so its entire configuration lived only inside the
running container. Removing that container destroyed the only record of how to
start it. The config now lives in files, and systemd owns restarts and boot.

---

## The daemon-image refresh timer

The provisioner launches per-org daemons from a **local** `kiwidaemon:latest`
tag and never pulls (see the `KIWI_DAEMON_IMAGE` note below). Something has to
keep that local tag current, and that is this timer — every 30 minutes, plus 2
minutes after boot so a host that was stopped overnight does not cold-start orgs
on a stale image.

```bash
sudo ./install-daemon-image-refresh.sh      # idempotent; re-run to pick up a change
systemctl list-timers kiwi-daemon-image.timer
journalctl -u kiwi-daemon-image.service -n 20
```

**It prunes, and that is not optional.** Retagging `kiwidaemon:latest` to a newly
pulled image leaves the previous one untagged. Those orphans are invisible to
`docker images` and accumulate one per refresh (~170MB each): the host reached
**98% and could not pull any image at all**, so cold starts failed with "no space
left on device" — which presents as a broken free tier, not as a disk alert.
Twenty-four orphans had built up before anyone looked.

The prune is `docker image prune -f` — **never `-a`**. Without `-a` it removes
only dangling images. With `-a` it would also delete every tagged image not
attached to a running container: `kiwidaemon:latest` itself, which the
provisioner resolves locally and cannot re-pull, and the sandbox base images
(`golang`, `node`, `postgres`) that a task would then refetch mid-run.

A prune failure is logged but does not fail the unit — serving a current daemon
image matters more than reclaiming space.

> The script and both units lived only on the host for their entire life, in no
> repository, exactly as the provisioner once did. A rebuilt or second fleet host
> would have silently served a stale image with nothing to say so. They are now
> in this directory and installed from it.

### Two settings that look wrong and are not

**Bridge networking, never `--network host`.** The metadata block above lives in
the `DOCKER-USER` chain, which iptables applies only to *forwarded* traffic —
bridge-networked containers. A host-networked container's traffic is host
`OUTPUT` and skips the chain entirely, so it can read the VM's service-account
token. Bridge reaches Cloud SQL over the VPC and the public Control Plane
without difficulty, so there is nothing gained by host networking and a
fleet-compromise vector lost.

**`KIWI_DAEMON_IMAGE` is deliberately unset.** Setting it to a registry
reference turns on `docker run --pull=always` for every per-org daemon launch.
Docker resolves registry credentials *client-side* — inside the provisioner
container — and that container is cut off from the metadata endpoint by
`harden-egress.sh`, so it cannot authenticate to the registry and every cold
start fails to pull. Left unset, the launcher uses the local `kiwidaemon:latest`
tag, which `kiwi-daemon-image.timer` refreshes on the **host**, where the
credentials live (installed by `install-daemon-image-refresh.sh` in this
directory). That is the design, not an oversight.
