package queue

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

const StreamName = "KIWI_JOBS"
const SubjectJobsSubmitted = "jobs.submitted"

// SetupStream creates or updates the JetStream stream for jobs.
func SetupStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{SubjectJobsSubmitted},
	})
	return err
}
