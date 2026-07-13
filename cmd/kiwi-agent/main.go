// Command kiwi-agent is the in-sandbox agent binary. It reaches the control
// plane ONLY through the Agent API, authenticated with the per-job scoped token
// injected at provision time — it never touches the control-plane database or
// the org API key directly (issue #34).
//
// This is the thin transport/entrypoint; the master/worker execution logic that
// runs on top of it lands in #35.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agentapi"
)

func main() {
	var (
		cpURL = flag.String("cp-url", os.Getenv("KIWI_CP_URL"), "control-plane base URL (or KIWI_CP_URL)")
		token = flag.String("token", os.Getenv("KIWI_JOB_TOKEN"), "scoped job token (or KIWI_JOB_TOKEN)")
		jobID = flag.String("job-id", os.Getenv("KIWI_JOB_ID"), "job id (or KIWI_JOB_ID)")
	)
	flag.Parse()

	if *cpURL == "" || *token == "" || *jobID == "" {
		fmt.Fprintln(os.Stderr, "kiwi-agent: --cp-url, --token and --job-id (or KIWI_CP_URL/KIWI_JOB_TOKEN/KIWI_JOB_ID) are required")
		os.Exit(2)
	}

	client := agentapi.NewClient(*cpURL, *token, *jobID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Announce startup on the durable event log via the scoped token.
	if _, err := client.AppendEvent(ctx, "agent_start", map[string]interface{}{"pid": os.Getpid()}); err != nil {
		fmt.Fprintf(os.Stderr, "kiwi-agent: append start event failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("kiwi-agent: reached control plane; scoped token accepted for job", *jobID)

	// Report a terminal result so the control plane can close the job. The real
	// Actor-Critic / master-worker loop that decides this status arrives in #35.
	if err := client.ReportResult(ctx, "SUCCEEDED", ""); err != nil {
		fmt.Fprintf(os.Stderr, "kiwi-agent: report result failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("kiwi-agent: reported terminal status")
}
