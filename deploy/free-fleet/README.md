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
