package provisioner

import (
	"context"
	"fmt"
	"os/exec"
)

// DockerLauncher implements Launcher using the local docker daemon.
type DockerLauncher struct{}

// NewDockerLauncher creates a new DockerLauncher.
func NewDockerLauncher() *DockerLauncher {
	return &DockerLauncher{}
}

func (d *DockerLauncher) containerName(orgID string) string {
	return fmt.Sprintf("kiwi-free-org-%s", orgID)
}

func (d *DockerLauncher) Launch(ctx context.Context, orgID, fleetID, joinToken, apiURL string) (Handle, error) {
	name := d.containerName(orgID)

	// In case there is an old container stuck, try to remove it first
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()

	// kiwidaemon docker run command
	cmd := exec.CommandContext(ctx, "docker", "run", "-d",
		"--name", name,
		"-e", "KIWI_JOIN_TOKEN="+joinToken,
		// Using a volume so cache survives restart (assuming kiwi-cache-orgID exists or will be created by docker)
		"-v", fmt.Sprintf("kiwi-cache-%s:/tmp/kiwi-cache", orgID),
		"kiwidaemon:latest",
		"-api-url", apiURL,
	)

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
