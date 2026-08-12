package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
)

// envFloat is the flag-default fallback for settings that must be reachable
// without changing the command line. The free-tier provisioner launches per-org
// daemons with a fixed argv (-api-url and -cache-dir only, see
// pkg/provisioner.launchArgs), so a flag alone is unreachable there; an env
// default is what makes the knob exist on the fleet at all. A malformed value
// falls back to def rather than failing the boot — a daemon that will not start
// is worse than one running the documented default.
func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		log.Printf("kiwidaemon: ignoring invalid %s=%q, using %.2f", key, v, def)
		return def
	}
	return f
}

func main() {
	var apiURL string
	var keyPath string
	var pollInterval time.Duration
	var cacheDir string
	var joinToken string
	var maxCachedRepos int
	var maxSteps int
	var maxRounds int
	var maxBudgetUSD float64
	var sessionBudgetUSD float64
	var sandboxRuntime string

	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	defaultKeyPath := filepath.Join(home, ".kiwi", "daemon.key")

	flag.StringVar(&apiURL, "api-url", "https://api.runkiwi.dev", "The URL of the Kiwi Control Plane API")
	flag.StringVar(&keyPath, "key-path", defaultKeyPath, "Path to load/save the X25519 private key.")
	flag.DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Base interval between Control Plane heartbeats (jitter and backoff are applied automatically).")
	flag.StringVar(&cacheDir, "cache-dir", "/tmp/kiwi-cache", "Path to store bare git repositories and worktrees.")
	flag.StringVar(&joinToken, "join-token", os.Getenv("KIWI_JOIN_TOKEN"), "Single-use join token to register this daemon (required on first boot; falls back to KIWI_JOIN_TOKEN).")
	flag.IntVar(&maxCachedRepos, "max-cached-repos", 20, "Max bare repositories to keep in the git cache before evicting the least-frequently-used (0 = unbounded).")
	flag.IntVar(&maxRounds, "max-rounds", 0, "Max Architect/Implementer rounds per task before giving up (0 = the session default).")
	// Deprecated aliases. The single-file loop these named is gone, but a
	// launcher that still passes them must keep starting: failing on an unknown
	// flag would take a fleet down for a flag that no longer decides anything.
	// -max-steps capped agent iterations, so it lands on the cap that replaced
	// it; -max-budget has a live successor under its own name and is ignored.
	flag.IntVar(&maxSteps, "max-steps", 0, "Deprecated alias for -max-rounds.")
	flag.Float64Var(&maxBudgetUSD, "max-budget", 0, "Deprecated and ignored; use -session-budget.")
	flag.Float64Var(&sessionBudgetUSD, "session-budget", envFloat("KIWI_SESSION_BUDGET_USD", 5.00), "Max provider spend (USD) per task (falls back to KIWI_SESSION_BUDGET_USD).")
	flag.StringVar(&sandboxRuntime, "sandbox-runtime", os.Getenv("KIWI_SANDBOX_RUNTIME"), "The OCI runtime to use for the docker sandbox (e.g. 'runsc').")
	flag.Parse()

	if maxRounds == 0 && maxSteps > 0 {
		maxRounds = maxSteps
	}
	if maxBudgetUSD > 0 {
		fmt.Fprintln(os.Stderr, "[kiwidaemon] -max-budget is ignored; per-task spend is capped by -session-budget.")
	}

	cfg := daemon.Config{
		APIURL:           apiURL,
		KeyPath:          keyPath,
		PollInterval:     pollInterval,
		CacheDir:         cacheDir,
		JoinToken:        joinToken,
		MaxCachedRepos:   maxCachedRepos,
		MaxRounds:        maxRounds,
		SessionBudgetUSD: sessionBudgetUSD,
		SandboxRuntime:   sandboxRuntime,
	}

	d, err := daemon.New(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize kiwidaemon: %v", err)
	}

	if err := d.Start(); err != nil {
		log.Fatalf("Fatal error starting kiwidaemon: %v", err)
	}

	// Setup context that cancels on SIGINT/SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Printf("Received signal: %v, shutting down...", sig)
		cancel()
	}()

	// Start the polling engine (blocks until context is canceled)
	if err := d.Run(ctx); err != nil {
		if err == context.Canceled {
			log.Println("Daemon shutdown complete.")
		} else {
			log.Fatalf("Daemon run error: %v", err)
		}
	}
}
