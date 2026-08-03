package provisioner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
)

// defaultDaemonImage is the image the launcher runs when KIWI_DAEMON_IMAGE is
// unset. In production point KIWI_DAEMON_IMAGE at the Artifact Registry path
// (e.g. REGION-docker.pkg.dev/PROJECT/REPO/kiwidaemon:latest) so no local retag
// is needed.
const defaultDaemonImage = "kiwidaemon:latest"

// dockerSocket is bind-mounted into each daemon container so the daemon can run
// its test-command sandbox as a sibling container under gVisor. See the trust
// note in Launch.
const dockerSocket = "/var/run/docker.sock"

// DockerLauncher implements Launcher using the local docker daemon.
type DockerLauncher struct {
	image string
	// pullAlways forces a registry pull on every launch. `docker run` reuses a
	// locally cached tag without checking the registry, so with a moving tag like
	// :latest a host that has run once will keep starting the OLD daemon image
	// forever — a deploy appears to succeed and changes nothing. Set only when the
	// image comes from a registry (see NewDockerLauncher).
	pullAlways bool
}

// NewDockerLauncher creates a new DockerLauncher. The daemon image is taken from
// KIWI_DAEMON_IMAGE (default "kiwidaemon:latest").
//
// An explicitly configured image is treated as a registry reference and pulled
// on every launch, so pushing a new build is enough to roll the daemons. The
// default is not: local development builds `kiwidaemon:latest` on the host and
// never pushes it anywhere, so forcing a pull there would fail every launch
// trying to reach Docker Hub.
func NewDockerLauncher() *DockerLauncher {
	img := os.Getenv("KIWI_DAEMON_IMAGE")
	if img == "" {
		return &DockerLauncher{image: defaultDaemonImage}
	}
	return &DockerLauncher{image: img, pullAlways: true}
}

func (d *DockerLauncher) containerName(orgID string) string {
	return fmt.Sprintf("kiwi-free-org-%s", orgID)
}

// hostCacheRoot is the directory on the fleet host under which each org's
// daemon keeps its bare clones and worktrees. Overridable for hosts that keep
// state elsewhere.
func hostCacheRoot() string {
	if v := os.Getenv("KIWI_HOST_CACHE_ROOT"); v != "" {
		return v
	}
	return "/var/lib/kiwi"
}

// cacheDirFor is the per-org cache path. It is used verbatim on BOTH sides of
// the daemon's bind mount — see launchArgs for why that is the whole point —
// and being per-org keeps one tenant's checkouts off another's filesystem.
func cacheDirFor(orgID string) string {
	return filepath.Join(hostCacheRoot(), orgID)
}

// launchArgs builds the `docker run` arguments for an org's daemon.
//
// The bind mount here is load-bearing and was previously a named volume, which
// silently broke every task on the free tier.
//
// The daemon writes a worktree to <cache>/worktrees/<task>, then runs the test
// command by asking the HOST docker daemon (via the mounted socket) to bind
// mount that path into a sandbox container. The host resolves the path against
// its own filesystem. With a named volume the daemon's /tmp/kiwi-cache existed
// only inside the daemon container, so the host found nothing at that path,
// created an empty directory, and mounted that — and every test command ran
// against an empty workspace. It could not pass, no edit could make it pass,
// and the failure surfaced as "reached max steps without passing".
//
// Mounting a host directory at the identical path inside the container makes
// the path mean the same thing on both sides, which is what a sibling-container
// setup requires. -cache-dir points the daemon at it.
func launchArgs(name, image, orgID, fleetID, joinToken, apiURL string, pullAlways bool) []string {
	args := []string{"run", "-d",
		"--name", name,
		"-e", "KIWI_JOIN_TOKEN=" + joinToken,
	}

	if pullAlways {
		args = append(args, "--pull=always")
	}

	if fleetID == auth.SharedFreeFleet {
		// Free daemons run the untrusted test command under gVisor.
		args = append(args, "-e", "KIWI_SANDBOX_RUNTIME=runsc")
	}

	cacheDir := cacheDirFor(orgID)
	args = append(args,
		"-v", dockerSocket+":"+dockerSocket,
		"-v", cacheDir+":"+cacheDir,
		image,
		"-api-url", apiURL,
		"-cache-dir", cacheDir,
	)
	return args
}

func (d *DockerLauncher) Launch(ctx context.Context, orgID, fleetID, joinToken, apiURL string, orgIdle bool) (Handle, error) {
	name := d.containerName(orgID)

	// A running container is left alone.
	//
	// The container is named per ORG, not per task, and this used to begin with
	// an unconditional `docker rm -f`. So submitting a second task while the
	// first was still running killed the daemon executing it — mid-edit,
	// mid-test, whenever. The task stayed LEASED with nobody running it, and
	// nothing detects a vanished daemon, so it sat until the 10-minute lease
	// lapsed (leaseTTL in pkg/orchestrator/daemon_api.go) and was re-leased on a
	// second attempt.
	//
	// The visible symptom was a task that took twelve minutes to do two minutes
	// of work, with the extra ten spent on a lease owned by a dead process. The
	// tell is attempts=2 on the slow ones and attempts=1 on the fast ones.
	//
	// Reuse is correct rather than merely safe: the daemon is a long-lived
	// per-org poller, so the running one picks the new task up on its next
	// heartbeat. Launching is only needed when nothing is there.
	if d.isRunning(ctx, name) {
		// Reuse unless the container is left over from a previous deploy AND the
		// org is idle. Skipping the staleness check entirely was a regression:
		// the old unconditional `rm -f` recycled the container on every submit,
		// so an image roll landed by side effect. Reuse removed that, and a
		// long-lived daemon then served the previous build indefinitely — a
		// deploy that reported success and changed nothing.
		//
		// Staleness alone is not enough to justify replacing it. A daemon on an
		// old image that is mid-task is exactly the case the reuse fix exists
		// for, so a busy one is left alone and retired on a later launch.
		if !d.isStale(ctx, name) || !orgIdle {
			return Handle(name), nil
		}
	}

	// Not running — but a stopped or half-created container still owns the name,
	// so it has to go before a fresh one can take it. This is the case the
	// original `rm -f` was written for.
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()

	// The host side of the bind mount must exist and be writable by the daemon
	// before it starts; docker would otherwise create it as root-owned.
	if err := os.MkdirAll(cacheDirFor(orgID), 0o777); err != nil {
		return "", fmt.Errorf("failed to create cache dir for %s: %w", orgID, err)
	}

	args := launchArgs(name, d.image, orgID, fleetID, joinToken, apiURL, d.pullAlways)

	// The daemon runs its test-command sandbox via `docker run`, so it needs a
	// Docker endpoint. We bind-mount the host socket, making test sandboxes
	// sibling containers on the free-fleet host (isolated per-sandbox by gVisor).
	//
	// Trust note: mounting docker.sock gives the daemon container control of the
	// host Docker daemon. The daemon runs Kiwi's own code (the untrusted,
	// model-generated code runs only inside the gVisor sandbox it launches), and
	// the free-fleet host is already treated as hostile-by-default (segmented,
	// no ambient cloud creds). The hardened alternative — a remote launcher that
	// keeps the provisioner off the execution host — is tracked as follow-up.
	//
	// CombinedOutput, not Run: docker reports *why* a launch failed on stderr
	// ("manifest unknown", "no such image", a denied pull), and Run discards it,
	// leaving only "exit status 125" to diagnose from.
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to launch docker container %s: %w: %s", name, err, firstLine(out))
	}

	return Handle(name), nil
}

// isRunning reports whether the org's container exists and is running.
//
// `docker inspect` exits non-zero when the container does not exist, which is
// indistinguishable here from a docker that is unreachable — both answer "not
// running", and both lead to the same next step of removing the name and
// launching. Erring toward false keeps the old behaviour when docker is sick:
// a launch that fails loudly beats silently reusing a container we cannot see.
func (d *DockerLauncher) isRunning(ctx context.Context, name string) bool {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// isStale reports whether the running container was started from a different
// image than the one this launcher would use now.
//
// Compared by resolved image ID, not by tag: the tag is what moves during a
// deploy, so both sides would read "kiwidaemon:latest" and always look equal.
func (d *DockerLauncher) isStale(ctx context.Context, name string) bool {
	running, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.Image}}", name).Output()
	if err != nil {
		return false
	}
	want, err := exec.CommandContext(ctx, "docker", "image", "inspect", "-f", "{{.Id}}", d.image).Output()
	if err != nil {
		// The desired image is not present locally, so there is nothing better
		// to switch to; keep what is running.
		return false
	}
	return strings.TrimSpace(string(running)) != strings.TrimSpace(string(want))
}

func (d *DockerLauncher) Stop(ctx context.Context, orgID string) error {
	name := d.containerName(orgID)
	out, err := exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop/remove docker container %s: %w: %s", name, err, firstLine(out))
	}
	return nil
}

// firstLine reduces command output to its first non-empty line, capped so a
// verbose failure cannot balloon the error text that gets persisted and shown to
// the user. Docker puts the actionable message on the first line.
func firstLine(out []byte) string {
	const maxLen = 300
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "no output"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}
