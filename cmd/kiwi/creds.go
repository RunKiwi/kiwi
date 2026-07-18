package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/client"
)

func runCreds(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("creds subcommand required: set")
	}

	sub := args[0]
	switch sub {
	case "set":
		return runCredsSet(args[1:])
	default:
		return fmt.Errorf("unknown creds subcommand: %s", sub)
	}
}

func runCredsSet(args []string) error {
	fs := flag.NewFlagSet("creds set", flag.ExitOnError)
	kind := fs.String("kind", "generic", "Kind of credential (e.g. llm, git)")
	token := fs.String("token", "", "Control plane API token (defaults to KIWI_SERVER_TOKEN or config)")
	server := fs.String("server", "http://localhost:8080", "Control plane URL")
	_ = fs.Parse(args)

	if fs.NArg() < 2 {
		return fmt.Errorf("usage: kiwi creds set <name> <value> [-kind <kind>]")
	}

	nameAlias := fs.Arg(0)
	value := fs.Arg(1)

	credName := nameAlias
	credKind := *kind

	if nameAlias == "anthropic" {
		credName = "ANTHROPIC_API_KEY"
		if credKind == "generic" {
			credKind = "llm"
		}
	} else if nameAlias == "git" {
		credName = "GIT_TOKEN"
		if credKind == "generic" {
			credKind = "git"
		}
	}

	apiToken := resolveToken(*token)
	if apiToken == "" {
		return fmt.Errorf("authentication required: use -token, KIWI_SERVER_TOKEN, or login")
	}

	if err := requireSecureRemote(*server); err != nil {
		return err
	}

	c := client.New(*server, apiToken)
	err := c.SetCredential(context.Background(), credName, credKind, value)
	if err != nil {
		return fmt.Errorf("failed to set credential: %w", err)
	}

	fmt.Printf("Successfully set credential %s\n", credName)
	return nil
}
