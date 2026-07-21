package provisioner

import (
	"context"
	"fmt"
	"os"
	"os/exec"

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
}

// NewDockerLauncher creates a new DockerLauncher. The daemon image is taken from
// KIWI_DAEMON_IMAGE (default "kiwidaemon:latest").
func NewDockerLauncher() *DockerLauncher {
	img := os.Getenv("KIWI_DAEMON_IMAGE")
	if img == "" {
		img = defaultDaemonImage
	}
	return &DockerLauncher{image: img}
}

func (d *DockerLauncher) containerName(orgID string) string {
	return fmt.Sprintf("kiwi-free-org-%s", orgID)
}

func (d *DockerLauncher) Launch(ctx context.Context, orgID, fleetID, joinToken, apiURL string) (Handle, error) {
	name := d.containerName(orgID)

	// In case there is an old container stuck, try to remove it first.
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()

	args := []string{"run", "-d",
		"--name", name,
		"-e", "KIWI_JOIN_TOKEN=" + joinToken,
	}

	if fleetID == auth.SharedFreeFleet {
		// Free daemons run the untrusted test command under gVisor.
		args = append(args, "-e", "KIWI_SANDBOX_RUNTIME=runsc")
	}

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
	args = append(args,
		"-v", dockerSocket+":"+dockerSocket,
		"-v", fmt.Sprintf("kiwi-cache-%s:/tmp/kiwi-cache", orgID),
		d.image,
		"-api-url", apiURL,
	)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to launch docker container %s: %w", name, err)
	}

	return Handle(name), nil
}

func (d *DockerLauncher) Stop(ctx context.Context, orgID string) error {
	name := d.containerName(orgID)
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop/remove docker container %s: %w", name, err)
	}
	return nil
}
