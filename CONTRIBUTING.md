# Contributing to Kiwi

## Before you start

Read [CLAUDE.md](CLAUDE.md). It has the architecture, the pre-commit checks CI
enforces, and the constraints that are easy to break without noticing.

## Sign your commits (DCO)

Every commit must carry a `Signed-off-by` line:

```bash
git commit -s -m "your message"
```

This is the [Developer Certificate of Origin](https://developercertificate.org/)
— the same one the Linux kernel uses. It is a statement that you wrote the
change, or have the right to submit it, and that you are contributing it under
the repository's licences.

We ask for it because Kiwi is dual-licensed. Code outside `ee/` is Apache-2.0;
code in `ee/` is BSL 1.1 (see [ee/LICENSE](ee/LICENSE)). Without a clear record
that contributors submitted their work under those terms, the licence on any
file they touched becomes unclear, and unpicking that after the fact means
tracking down every past contributor. The sign-off costs you one flag and
avoids that entirely.

If you contribute to `ee/`, you are contributing under BSL 1.1 and granting
RunKiwi the right to relicense that contribution — including commercially.
If you would rather not, contribute outside `ee/`; almost everything
interesting about how Kiwi works lives there.

## Where your change goes

The split is not arbitrary, and a change on the wrong side of it is the one
review comment guaranteed to come back:

- **Outside `ee/`** — the execution engine and everything a customer runs in
  their own cloud. The Actor–Critic loop, the sandbox, provider clients, the
  BYOC daemon, the CLI, the signed execution record, model discovery.
- **`ee/`** — the multi-tenant Control Plane. Orchestration, planning, orgs and
  auth, billing, entitlements, provisioning, fleet control.

`ee/` may import Apache-2.0 packages. **The reverse is not allowed** — it would
pull BSL terms into code we tell people is Apache-2.0. `pkg/licensing_boundary_test.go`
enforces this and will fail your build with an explanation. If you need
Control-Plane behaviour from an Apache-2.0 package, define an interface in the
Apache-2.0 package and let `ee/` supply the implementation.

## Before every commit

```bash
gofmt -l cmd/ pkg/ ee/          # must print nothing
CGO_ENABLED=0 go vet ./...      # must be clean
CGO_ENABLED=0 go test ./...     # must pass
CGO_ENABLED=0 go build ./...    # must build
```

Frontend changes also need `npm test` and `npm run build` in `frontend/`.

## Pull requests

- Ship tests with the change. New behaviour without a test that fails when the
  behaviour is removed is not finished.
- Keep `README.md` current, or add the `skip-readme-check` label.
- Say what you verified and how. If something is untested or you were unsure,
  say that too — it is far cheaper to hear it in the description than to find it
  in review.
