// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import "testing"

func TestWantsIssueCreationDetectsExplicitAsk(t *testing.T) {
	if !wantsIssueCreation("investigate this and create a github issue") {
		t.Fatal("expected an explicit ask to be detected")
	}
	if wantsIssueCreation("investigate this bug") {
		t.Fatal("expected no issue creation without an explicit ask")
	}
}
