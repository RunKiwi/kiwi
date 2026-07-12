package infra

import (
	"context"
	"errors"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrTestFailed     = errors.New("test failed")
)

// Infra defines the abstraction for provisioning and managing isolated sandboxes.
type Infra interface {
	Provision(ctx context.Context, sandboxPath string, manifest *store.Manifest) (Handle, error)
	Status(ctx context.Context, handle Handle) (string, error)
	Snapshot(ctx context.Context, handle Handle) (*store.SnapshotRef, error)
	Restore(ctx context.Context, handle Handle, ref *store.SnapshotRef) error
	Terminate(ctx context.Context, handle Handle) error
}

// Handle represents an active execution environment.
type Handle interface {
	ID() string
	RunCommand(ctx context.Context, cmd string, env []string) (string, error)
	GetOutputArchive(ctx context.Context) ([]byte, error)
}
