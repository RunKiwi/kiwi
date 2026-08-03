package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/client"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
)

func runSubmit(args []string) error {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "kiwid daemon URL")
	token := fs.String("token", "", "Bearer auth token (defaults to KIWI_SERVER_TOKEN or config)")
	task := fs.String("task", "", "task/goal description")
	file := fs.String("file", "", "target file (relative to -dir, or to the repo root with -repo)")
	testCmd := fs.String("test-cmd", "", "test command the daemon runs")
	dir := fs.String("dir", ".", "directory to zip and upload (legacy path; ignored with -repo)")
	repo := fs.String("repo", "", "git repo URL — BYOC path: the daemon clones it in your cloud and runs via the lease queue")
	ref := fs.String("ref", "main", "git ref to check out (with -repo)")
	model := fs.String("model", "", "LLM model for the worker (with -repo; planner default if empty)")
	mode := fs.String("mode", "", "execution loop: file_loop (default) or session — session runs an Architect that plans and reviews an agentic Implementer over several rounds")
	architectModel := fs.String("architect-model", "", "model that plans and reviews in -mode session (defaults to -model); expected to be more capable than the worker")
	maxWorkers := fs.Int("max-workers", 1, "workers the planner may fan out (with -repo; 1 = single-worker MVP path)")
	secretsPath := fs.String("secrets", "secrets.json", "path to secrets.json")
	resume := fs.Bool("resume", false, "resume an existing task instead of submitting")
	taskID := fs.String("task-id", "", "task ID to resume (with -resume)")
	interval := fs.Duration("interval", 2*time.Second, "status poll interval")
	idempotencyKey := fs.String("idempotency-key", "", "optional Idempotency-Key to dedupe retried submissions")

	_ = fs.Parse(args)

	t := resolveToken(*token)

	// -repo selects the BYOC path (planner → lease queue → daemon in your cloud).
	// Without it, the legacy zip-upload path runs the task in the Control Plane.
	if *repo != "" {
		return submitPlan(*server, t, *idempotencyKey, *interval, client.PlanOptions{
			Task:           *task,
			RepoURL:        *repo,
			Ref:            *ref,
			File:           *file,
			TestCmd:        *testCmd,
			Model:          *model,
			MaxWorkers:     *maxWorkers,
			Mode:           *mode,
			ArchitectModel: *architectModel,
		})
	}

	return submitTask(*server, t, *idempotencyKey, *task, *file, *testCmd, *dir, *secretsPath, *resume, *taskID, *interval)
}

// validateMode rejects an unknown -mode at the CLI rather than letting it reach
// the planner, which treats anything that is not "session" as file_loop. A typo
// like -mode sesion would otherwise run the default loop and report success,
// and the user would have no way to tell their flag was ignored.
func validateMode(mode string) error {
	switch mode {
	case "", agent.ModeFileLoop, agent.ModeSession:
		return nil
	default:
		return fmt.Errorf("unknown -mode %q: use %q or %q", mode, agent.ModeFileLoop, agent.ModeSession)
	}
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// requireSecureRemote refuses to send a bearer token in cleartext to a non-local
// host.
func requireSecureRemote(server string) error {
	u, err := url.Parse(server)
	if err != nil {
		return fmt.Errorf("invalid server url: %w", err)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return fmt.Errorf("refusing to send token over cleartext HTTP to remote server %s. Use HTTPS", server)
	}
	return nil
}

// submitPlan drives the BYOC path: it submits the task to the Control Plane
// planner, which decomposes it and enqueues workers onto the lease queue. A
// registered daemon in the customer's cloud leases and executes them. No
// codebase is uploaded — the daemon clones repo directly.
func submitPlan(server, token, idempotencyKey string, interval time.Duration, opts client.PlanOptions) error {
	if err := requireSecureRemote(server); err != nil {
		return err
	}
	if opts.Task == "" || opts.File == "" || opts.TestCmd == "" {
		return fmt.Errorf("-task, -file, and -test-cmd are required")
	}
	if err := validateMode(opts.Mode); err != nil {
		return err
	}

	c := client.New(server, token)
	c.IdempotencyKey = idempotencyKey
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res, err := c.PlanTask(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to submit plan: %w", err)
	}

	fmt.Printf("[kiwi] Plan submitted: %s\n", res.Summary)
	fmt.Printf("[kiwi] Job:      %s\n", res.JobID)
	fmt.Printf("[kiwi] Manifest: %s\n", res.ManifestID)
	fmt.Printf("[kiwi] Enqueued %d worker task(s): %v\n", len(res.TaskIDs), res.TaskIDs)
	if opts.Mode == agent.ModeSession {
		architect := opts.ArchitectModel
		if architect == "" {
			architect = opts.Model
		}
		// Worth saying out loud: session mode is the more expensive path, and the
		// two-model split is the thing people get wrong when they first reach for it.
		fmt.Printf("[kiwi] Mode:     session — architect %s plans and reviews, implementer %s works in rounds\n",
			orDefault(architect, "planner default"), orDefault(opts.Model, "planner default"))
	}
	fmt.Printf("[kiwi] A registered daemon in your cloud will lease and execute these. "+
		"It clones %s@%s, runs the loop until %q passes, and opens a PR.\n", opts.RepoURL, opts.Ref, opts.TestCmd)

	for {
		status, err := c.GetJob(ctx, res.JobID)
		if err != nil {
			return fmt.Errorf("failed to get job status: %w", err)
		}

		allTerminal := true
		anyFailed := false
		for _, t := range status.Tasks {
			if t.Status != "SUCCEEDED" && t.Status != "FAILED" {
				allTerminal = false
				break
			}
			if t.Status == "FAILED" {
				anyFailed = true
			}
		}

		if allTerminal {
			fmt.Printf("\n[kiwi] Job %s complete:\n", res.JobID)
			for _, t := range status.Tasks {
				if t.Status == "SUCCEEDED" {
					url := ""
					if t.ResultURL != nil {
						url = *t.ResultURL
					}
					fmt.Printf("✓ %s → %s\n", t.ID, url)
				} else {
					detail := ""
					if t.ResultDetail != nil {
						detail = *t.ResultDetail
					}
					fmt.Printf("✗ %s FAILED: %s\n", t.ID, detail)
				}
			}
			if anyFailed {
				return fmt.Errorf("job %s failed", res.JobID)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
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
