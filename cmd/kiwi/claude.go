package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func runClaude(args []string) error {
	fs := flag.NewFlagSet("claude", flag.ExitOnError)
	_ = fs.Parse(args)

	fmt.Println("[kiwi] Starting Claude Code with Kiwi Swarm capabilities...")

	sysPrompt := `You are running within the Kiwi BYOC execution environment. 
To offload large parallel tasks to the Swarm, you can use the 'kiwi submit' command.
Example: kiwi submit -task "Migrate all tables" -file "schema.sql" -test-cmd "make test" -dir "."
Wait for the task to complete.`

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found in PATH: %w", err)
	}

	cmdArgs := append([]string{"--append-system-prompt", sysPrompt}, fs.Args()...)
	cmd := exec.Command(claudePath, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("claude CLI exited with error: %w", err)
	}

	return nil
}
