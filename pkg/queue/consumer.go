package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/manifest"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/nats-io/nats.go/jetstream"
)

type Consumer struct {
	js       jetstream.JetStream
	db       store.Store
	launchFn func(taskID, sandboxPath, task, file, testCmd string)
}

func NewConsumer(js jetstream.JetStream, db store.Store, launchFn func(taskID, sandboxPath, task, file, testCmd string)) *Consumer {
	return &Consumer{
		js:       js,
		db:       db,
		launchFn: launchFn,
	}
}

func (c *Consumer) Start(ctx context.Context) error {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       "orchestrator_worker",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: SubjectJobsSubmitted,
		MaxDeliver:    5,
		AckWait:       1 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		c.handleMsg(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("failed to start consuming: %w", err)
	}

	go func() {
		<-ctx.Done()
		cc.Stop()
	}()

	return nil
}

func (c *Consumer) handleMsg(ctx context.Context, msg jetstream.Msg) {
	var payload map[string]interface{}
	if err := json.Unmarshal(msg.Data(), &payload); err != nil {
		fmt.Printf("[Consumer] Malformed payload: %v\n", err)
		msg.Term()
		return
	}

	jobID, ok := payload["job_id"].(string)
	if !ok {
		fmt.Printf("[Consumer] Missing job_id in payload\n")
		msg.Term()
		return
	}

	// Look up the job using V2 Store
	job, err := c.db.GetJob(ctx, jobID)
	if err != nil {
		fmt.Printf("[Consumer] Job %s not found: %v\n", jobID, err)
		msg.Nak()
		return
	}

	if job.SandboxRef == nil || *job.SandboxRef == "" {
		fmt.Printf("[Consumer] Job %s missing sandbox_ref\n", jobID)
		msg.Term()
		return
	}

	var wf *store.Workflow
	if job.WorkflowID != nil {
		wf = &store.Workflow{}
		if err := c.db.DB().WithContext(ctx).First(wf, "id = ?", *job.WorkflowID).Error; err != nil {
			fmt.Printf("[Consumer] Failed to resolve workflow %s: %v\n", *job.WorkflowID, err)
			msg.Nak()
			return
		}
	}

	// Generate immutable manifest
	m, err := manifest.Generate(job, wf)
	if err != nil {
		fmt.Printf("[Consumer] Failed to generate manifest for job %s: %v\n", jobID, err)
		msg.Nak()
		return
	}

	// Persist manifest
	if err := c.db.CreateManifest(ctx, m); err != nil {
		fmt.Printf("[Consumer] Failed to persist manifest for job %s: %v\n", jobID, err)
		msg.Nak()
		return
	}

	// Pin manifest to job
	if err := c.db.UpdateJobManifest(ctx, job.ID, m.ID); err != nil {
		fmt.Printf("[Consumer] Failed to pin manifest to job %s: %v\n", jobID, err)
		msg.Nak()
		return
	}

	task, _ := job.Inputs["task"].(string)
	file, _ := job.Inputs["file"].(string)
	testCmd, _ := job.Inputs["test_cmd"].(string)

	ok, errUpdate := c.db.UpdateJobStatus(ctx, job.ID, "PENDING", "SCHEDULING")
	if errUpdate != nil {
		fmt.Printf("[Consumer] Failed to update job status %s: %v\n", jobID, errUpdate)
		msg.Nak()
		return
	}
	if !ok {
		fmt.Printf("[Consumer] Job %s already processing or completed\n", jobID)
		msg.Ack()
		return
	}

	fmt.Printf("[Consumer] Launching job %s\n", jobID)
	c.launchFn(job.ID, *job.SandboxRef, task, file, testCmd)

	// The orchestrator has accepted it into background processing
	msg.Ack()
}
