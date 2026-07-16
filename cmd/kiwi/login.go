package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/term"
)

type Config struct {
	Token string `json:"token"`
}

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "kiwi_config.json"
	}
	return filepath.Join(home, ".config", "kiwi", "config.json")
}

func loadConfig() Config {
	var cfg Config
	b, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	token := fs.String("token", "", "Kiwi API token")
	_ = fs.Parse(args)

	t := *token
	if t == "" {
		fmt.Print("Enter Kiwi API Token: ")
		tBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		fmt.Println()
		t = string(tBytes)
	}

	if t == "" {
		return fmt.Errorf("token cannot be empty")
	}

	cfg := Config{Token: t}
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(p, b, 0600); err != nil {
		return err
	}

	fmt.Printf("[kiwi] Logged in successfully. Config saved to %s\n", p)
	return nil
}
