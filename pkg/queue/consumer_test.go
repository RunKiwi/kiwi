package queue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/nats-io/nats.go/jetstream"
)

type mockStore struct {
	store.Store
	jobStatusCalls int
}

func (m *mockStore) GetJob(ctx context.Context, id string) (*store.Job, error) {
	sandboxRef := "ref"
	return &store.Job{
		ID:         id,
		SandboxRef: &sandboxRef,
		Inputs:     map[string]interface{}{"task": "test"},
	}, nil
}

func (m *mockStore) CreateManifest(ctx context.Context, manifest *store.Manifest) error {
	return nil
}

func (m *mockStore) UpdateJobManifest(ctx context.Context, jobID, manifestID string) error {
	return nil
}

func (m *mockStore) UpdateJobStatus(ctx context.Context, id string, expectedStatus string, newStatus string) (bool, error) {
	m.jobStatusCalls++
	if m.jobStatusCalls == 1 {
		return true, nil // first time succeeds
	}
	return false, nil // second time fails (simulating redelivery where status already changed)
}

type mockMsg struct {
	jetstream.Msg
	data []byte
	acks int
}

func (m *mockMsg) Data() []byte { return m.data }
func (m *mockMsg) Ack() error   { m.acks++; return nil }
func (m *mockMsg) Nak() error   { return nil }
func (m *mockMsg) Term() error  { return nil }

func TestConsumerRedeliveryIdempotency(t *testing.T) {
	db := &mockStore{}
	launchCount := 0
	launchFn := func(taskID, sandboxPath, task, file, testCmd string) {
		launchCount++
	}

	c := NewConsumer(nil, db, launchFn)

	payload, _ := json.Marshal(map[string]interface{}{"job_id": "job-123"})

	// Simulate first delivery
	msg1 := &mockMsg{data: payload}
	c.handleMsg(context.Background(), msg1)

	if launchCount != 1 {
		t.Errorf("expected 1 launch, got %d", launchCount)
	}
	if msg1.acks != 1 {
		t.Errorf("expected msg to be acked once on first delivery, got %d", msg1.acks)
	}

	// Simulate redelivery (Jetstream at-least-once)
	msg2 := &mockMsg{data: payload}
	c.handleMsg(context.Background(), msg2)

	if launchCount != 1 {
		t.Errorf("expected launch to be skipped on redelivery, got %d", launchCount)
	}
	if msg2.acks != 1 {
		t.Errorf("expected msg to be acked on redelivery idempotency, got %d", msg2.acks)
	}
}
