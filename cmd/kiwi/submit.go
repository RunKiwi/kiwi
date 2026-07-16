package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/client"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
)

func runSubmit(args []string) error {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "kiwid daemon URL")
	token := fs.String("token", "", "Bearer auth token (defaults to KIWI_SERVER_TOKEN or config)")
	task := fs.String("task", "", "task/goal description")
	file := fs.String("file", "", "target file, relative to -dir")
	testCmd := fs.String("test-cmd", "", "test command the daemon runs")
	dir := fs.String("dir", ".", "directory to zip and upload")
	secretsPath := fs.String("secrets", "secrets.json", "path to secrets.json")
	resume := fs.Bool("resume", false, "resume an existing task instead of submitting")
	taskID := fs.String("task-id", "", "task ID to resume (with -resume)")
	interval := fs.Duration("interval", 2*time.Second, "status poll interval")
	idempotencyKey := fs.String("idempotency-key", "", "optional Idempotency-Key to dedupe retried submissions")

	_ = fs.Parse(args)

	t := resolveToken(*token)

	return submitTask(*server, t, *idempotencyKey, *task, *file, *testCmd, *dir, *secretsPath, *resume, *taskID, *interval)
}

func resolveToken(flagToken string) string {
	if flagToken != "" {
		return flagToken
	}
	if envToken := os.Getenv("KIWI_SERVER_TOKEN"); envToken != "" {
		return envToken
	}
	cfg := loadConfig()
	return cfg.Token
}

func submitTask(server, token, idempotencyKey, task, file, testCmd, dir, secretsPath string, resume bool, taskID string, interval time.Duration) error {
	u, err := url.Parse(server)
	if err != nil {
		return fmt.Errorf("invalid server url: %w", err)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return fmt.Errorf("refusing to send token over cleartext HTTP to remote server %s. Use HTTPS", server)
	}

	c := client.New(server, token)
	c.IdempotencyKey = idempotencyKey
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if resume {
		if taskID == "" {
			return fmt.Errorf("-resume requires -task-id")
		}
		fmt.Printf("[kiwi] Resuming task %s\n", taskID)
	} else {
		if task == "" || file == "" || testCmd == "" {
			return fmt.Errorf("-task, -file, and -test-cmd are required")
		}
		fmt.Printf("[kiwi] Packaging %s ...\n", dir)
		zipBytes, err := sandbox.ZipDir(dir)
		if err != nil {
			return fmt.Errorf("failed to package codebase: %w", err)
		}
		id, err := c.SubmitTask(ctx, task, file, testCmd, zipBytes)
		if err != nil {
			return fmt.Errorf("failed to submit task: %w", err)
		}
		taskID = id
		fmt.Printf("[kiwi] Submitted task %s\n", taskID)
	}

	// Serve the reverse credential tunnel in the background.
	go func() {
		_ = tunnel.ConnectAndListen(ctx, server, taskID, token, client.SecretLookup(secretsPath))
	}()

	// Poll status and stream logs until terminal state.
	prevLogs := ""
	for {
		st, err := c.GetStatus(ctx, taskID)
		if err != nil {
			return fmt.Errorf("failed to get status: %w", err)
		}
		if delta := client.LogDelta(prevLogs, st.Logs); delta != "" {
			fmt.Print(delta)
			prevLogs = st.Logs
		}
		switch st.Status {
		case "SUCCESS":
			fmt.Printf("\n[kiwi] Task SUCCESS (cost $%.4f)\n", st.Cost)
			out, err := c.DownloadResult(ctx, taskID)
			if err != nil {
				return fmt.Errorf("failed to download result: %w", err)
			}
			outPath := fmt.Sprintf("kiwi-fix-%s.zip", taskID)
			if err := os.WriteFile(outPath, out, 0644); err != nil {
				return fmt.Errorf("failed to write result: %w", err)
			}
			fmt.Printf("[kiwi] Fixed codebase saved to %s\n", outPath)
			return nil
		case "FAILED":
			return fmt.Errorf("task FAILED (see logs above)")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
