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

	cmd := exec.Command(claudePath, fs.Args()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	env := append(os.Environ(), fmt.Sprintf("CLAUDE_SYSTEM_PROMPT=%s", sysPrompt))
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude CLI exited with error: %w", err)
	}

	return nil
}
