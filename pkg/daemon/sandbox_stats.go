// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ContainerMemStats records the memory consumption and limit for a single sandbox container.
type ContainerMemStats struct {
	ContainerID string
	RSSMB       int64
	LimitMB     int64
}

// currentSandboxMemStats queries docker for memory usage of all running containers.
// It executes `docker stats --no-stream --format "{{.ID}} {{.MemUsage}}"` with a 5-second timeout.
func currentSandboxMemStats(ctx context.Context) ([]ContainerMemStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{.ID}} {{.MemUsage}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseDockerStatsOutput(string(out))
}

// parseDockerStatsOutput parses the output of `docker stats --no-stream --format "{{.ID}} {{.MemUsage}}"`.
// Each line is expected to have the format: `<container_id> <usage> / <limit>`.
func parseDockerStatsOutput(out string) ([]ContainerMemStats, error) {
	var stats []ContainerMemStats
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed docker stats line (missing '/'): %q", line)
		}
		leftFields := strings.Fields(parts[0])
		if len(leftFields) != 2 {
			return nil, fmt.Errorf("malformed docker stats line (expected container ID and usage): %q", line)
		}
		containerID := leftFields[0]
		rssMB, err := parseDockerMemSize(leftFields[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse memory usage %q in line %q: %w", leftFields[1], line, err)
		}
		limitStr := strings.TrimSpace(parts[1])
		limitMB, err := parseDockerMemSize(limitStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse memory limit %q in line %q: %w", limitStr, line, err)
		}
		stats = append(stats, ContainerMemStats{
			ContainerID: containerID,
			RSSMB:       rssMB,
			LimitMB:     limitMB,
		})
	}
	return stats, nil
}

// parseDockerMemSize converts a human-readable memory size string (e.g. "512.3MiB", "4GiB", "100KiB", "500B")
// to int64 whole megabytes.
func parseDockerMemSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty memory size")
	}

	// Find the boundary between the numeric value and the unit suffix.
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.' || s[i] == '-' || s[i] == '+') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid memory size: %q", s)
	}

	numStr := s[:i]
	unit := strings.TrimSpace(strings.ToLower(s[i:]))

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory size number %q in %q: %w", numStr, s, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("negative memory size: %q", s)
	}

	var mb float64
	switch unit {
	case "tib", "tb", "t":
		mb = val * 1024 * 1024
	case "gib", "gb", "g":
		mb = val * 1024
	case "mib", "mb", "m":
		mb = val
	case "kib", "kb", "k":
		mb = val / 1024
	case "b", "bytes", "byte", "":
		mb = val / (1024 * 1024)
	default:
		return 0, fmt.Errorf("unknown memory unit %q in %q", unit, s)
	}

	return int64(mb), nil
}
