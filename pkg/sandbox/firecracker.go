package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Firecracker defaults
const (
	defaultKernelPath = "/var/lib/kiwi/firecracker/vmlinux"
	defaultRootfsPath = "/var/lib/kiwi/firecracker/rootfs.ext4"
	firecrackerBinary = "firecracker"
)

// runFirecracker boots a Firecracker microVM, runs the command, and captures output.
func runFirecracker(ctx context.Context, dir string, cmdStr string, env []string, cfg *SandboxConfig) (*Result, error) {
	// 1. Prepare a block device for the workspace
	workImg := filepath.Join(os.TempDir(), fmt.Sprintf("kiwi-work-%d.ext4", time.Now().UnixNano()))
	defer os.Remove(workImg)

	// Create a 512MB ext4 image (adjust as needed, keeping it simple for now)
	if err := exec.Command("dd", "if=/dev/zero", "of="+workImg, "bs=1M", "count=512").Run(); err != nil {
		return nil, fmt.Errorf("failed to create workspace image: %w", err)
	}
	if err := exec.Command("mkfs.ext4", "-F", workImg).Run(); err != nil {
		return nil, fmt.Errorf("failed to format workspace image: %w", err)
	}

	// Mount the image to copy files in
	mountDir, err := os.MkdirTemp("", "kiwi-mount-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create mount dir: %w", err)
	}
	defer os.RemoveAll(mountDir)

	if err := exec.Command("sudo", "mount", "-o", "loop", workImg, mountDir).Run(); err != nil {
		return nil, fmt.Errorf("failed to mount workspace image: %w", err)
	}

	// Copy workspace contents (requires rsync or cp -a)
	// We use sudo because the mount is owned by root
	if err := exec.Command("sudo", "cp", "-a", dir+"/.", mountDir+"/").Run(); err != nil {
		exec.Command("sudo", "umount", mountDir).Run()
		return nil, fmt.Errorf("failed to copy workspace: %w", err)
	}

	// Create init script
	initScript := fmt.Sprintf(`#!/bin/sh
%s
%s
`, strings.Join(env, " "), cmdStr)
	scriptPath := filepath.Join(mountDir, "kiwi_init.sh")
	if err := os.WriteFile(scriptPath, []byte(initScript), 0755); err == nil {
		exec.Command("sudo", "chown", "root:root", scriptPath).Run()
	}

	// Unmount
	if err := exec.Command("sudo", "umount", mountDir).Run(); err != nil {
		return nil, fmt.Errorf("failed to unmount workspace image: %w", err)
	}

	// 2. Prepare Firecracker config
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("kiwi-fc-%d.sock", time.Now().UnixNano()))
	defer os.Remove(socketPath)

	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("kiwi-fc-%d.log", time.Now().UnixNano()))
	defer os.Remove(logPath)

	config := map[string]interface{}{
		"boot-source": map[string]interface{}{
			"kernel_image_path": defaultKernelPath,
			"boot_args":         `console=ttyS0 reboot=k panic=1 pci=off init=/bin/sh -c "mkdir -p /workspace && mount /dev/vdb /workspace && cd /workspace && ./kiwi_init.sh; sync; poweroff -f"`,
		},
		"drives": []map[string]interface{}{
			{
				"drive_id":       "rootfs",
				"path_on_host":   defaultRootfsPath,
				"is_root_device": true,
				"is_read_only":   true,
			},
			{
				"drive_id":       "workspace",
				"path_on_host":   workImg,
				"is_root_device": false,
				"is_read_only":   false,
			},
		},
		"machine-config": map[string]interface{}{
			"vcpu_count":   1,
			"mem_size_mib": 512,
		},
	}

	if cfg != nil && cfg.CPULimit != "" {
		// simplistic mapping, real production would parse
		config["machine-config"].(map[string]interface{})["vcpu_count"] = 2
	}
	if cfg != nil && cfg.MemoryLimit != "" {
		config["machine-config"].(map[string]interface{})["mem_size_mib"] = 1024
	}

	configFile := filepath.Join(os.TempDir(), fmt.Sprintf("kiwi-fc-cfg-%d.json", time.Now().UnixNano()))
	defer os.Remove(configFile)

	b, _ := json.Marshal(config)
	if err := os.WriteFile(configFile, b, 0644); err != nil {
		return nil, fmt.Errorf("failed to write fc config: %w", err)
	}

	// 3. Run Firecracker
	// Firecracker runs synchronously when passed --config-file, starting immediately if
	// auto-start is implicit, but wait, usually we need to call the API or pass --config-file
	// In newer firecracker, --config-file with no API socket might start it automatically.
	// But let's just use --api-sock and --config-file (which auto-starts).
	fcCmd := exec.CommandContext(ctx, firecrackerBinary, "--api-sock", socketPath, "--config-file", configFile)

	var outBuf bytes.Buffer
	fcCmd.Stdout = &outBuf
	fcCmd.Stderr = &outBuf

	err = fcCmd.Run()
	output := outBuf.String()

	// If the context was canceled, Firecracker might have been killed, which is fine.

	// 4. Capture diff
	// Mount again to diff
	if err := exec.Command("sudo", "mount", "-o", "loop", workImg, mountDir).Run(); err == nil {
		defer exec.Command("sudo", "umount", mountDir).Run()

		diffCmd := exec.CommandContext(ctx, "git", "diff")
		diffCmd.Dir = mountDir
		var diffBuf bytes.Buffer
		diffCmd.Stdout = &diffBuf
		_ = diffCmd.Run()

		return &Result{
			Success: err == nil, // We'll consider any error from firecracker as failure
			Output:  output,
			GitDiff: diffBuf.String(),
		}, nil
	}

	return &Result{
		Success: err == nil,
		Output:  output,
		GitDiff: "",
	}, nil
}
