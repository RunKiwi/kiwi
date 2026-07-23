#!/usr/bin/env bash
#
# Verify (and demo) the free-fleet egress hardening from a container's point of
# view: the metadata token endpoint must be UNREACHABLE, while DNS + public
# internet must still WORK (the daemon depends on them). Exits non-zero if any
# expectation fails, so it doubles as a post-deploy check.
#
# Usage:  ./verify-egress.sh
set -uo pipefail

IMG="${IMG:-curlimages/curl:8.10.1}"
META_URL="http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token"
PUBLIC_URL="${PUBLIC_URL:-https://api.anthropic.com}"
fail=0

run() { docker run --rm "$IMG" "$@"; }

echo "== Free-fleet egress verification =="

echo "1) Metadata token endpoint must be BLOCKED:"
if run -s --max-time 4 -H "Metadata-Flavor: Google" "$META_URL" >/dev/null 2>&1; then
  echo "   ❌ REACHABLE — a container could steal the VM service-account token"
  fail=1
else
  echo "   ✅ blocked"
fi

echo "2) Public internet egress must WORK (Control Plane + LLM APIs):"
if run -s --max-time 8 -o /dev/null "$PUBLIC_URL" >/dev/null 2>&1; then
  echo "   ✅ reachable ($PUBLIC_URL)"
else
  echo "   ❌ blocked — the daemon can't reach its APIs (check DNS / rules)"
  fail=1
fi

echo
if [ "$fail" -eq 0 ]; then
  echo "PASS — model code is boxed in, the daemon can still work."
else
  echo "FAIL — see above; re-run ./harden-egress.sh and check DNS is intact."
fi
exit "$fail"
