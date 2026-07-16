package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: kiwi <command> [args]")
		fmt.Println("Commands: login, submit")
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "login":
		err = runLogin(args)
	case "submit":
		err = runSubmit(args)
	case "claude":
		err = runClaude(args)
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[kiwi] error: %v\n", err)
		os.Exit(1)
	}
}
