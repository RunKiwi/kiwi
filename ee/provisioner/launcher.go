// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package provisioner

import (
	"context"
	"sync"
)

// Handle is an opaque identifier for a running per-org daemon process.
type Handle string

// Launcher provides an interface to start and stop per-org daemon processes.
type Launcher interface {
	// Launch starts a per-org daemon process for orgID, bound to fleetID,
	// presenting joinToken on first handshake. Returns an opaque handle.
	// orgIdle reports that the org has no task in flight, which is what makes
	// it safe to retire a container left on a previous image. Killing a busy
	// daemon strands its lease for the full TTL, so a stale-but-working
	// container is kept when orgIdle is false and retired on a later launch.
	Launch(ctx context.Context, orgID, fleetID, joinToken, apiURL string, orgIdle bool) (Handle, error)
	Stop(ctx context.Context, orgID string) error
}

// StubLauncher records calls and acts as a fake Launcher for tests.
type StubLauncher struct {
	mu            sync.Mutex
	LaunchCalls   []LaunchCall
	StopCalls     []string
	LaunchErr     error
	StopErr       error
	HandleCounter int
}

type LaunchCall struct {
	OrgID     string
	FleetID   string
	JoinToken string
	APIURL    string
}

// NewStubLauncher creates a new StubLauncher.
func NewStubLauncher() *StubLauncher {
	return &StubLauncher{}
}

func (s *StubLauncher) Launch(ctx context.Context, orgID, fleetID, joinToken, apiURL string, orgIdle bool) (Handle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.LaunchErr != nil {
		return "", s.LaunchErr
	}

	s.LaunchCalls = append(s.LaunchCalls, LaunchCall{
		OrgID:     orgID,
		FleetID:   fleetID,
		JoinToken: joinToken,
		APIURL:    apiURL,
	})

	s.HandleCounter++
	return Handle("stub_handle_" + orgID), nil
}

func (s *StubLauncher) Stop(ctx context.Context, orgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.StopErr != nil {
		return s.StopErr
	}

	s.StopCalls = append(s.StopCalls, orgID)
	return nil
}
