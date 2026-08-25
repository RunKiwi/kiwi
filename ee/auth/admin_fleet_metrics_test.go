// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

func newColdStartTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&store.QueuedTask{}, &store.Fleet{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// A task that predates this column (or whose measurement genuinely couldn't
// be confirmed) defaults to 0 — the regression this guards is that value
// silently dragging the average toward a number nothing actually measured.
func TestLoadColdStartMetrics_UnmeasuredRowsExcluded(t *testing.T) {
	db := newColdStartTestDB(t)
	tasks := []store.QueuedTask{
		{ID: "t1", OrgID: "o1", SandboxProvisionMs: 400, SandboxImage: "golang:1.25-alpine", CreatedAt: time.Now()},
		{ID: "t2", OrgID: "o1", SandboxProvisionMs: 0, SandboxImage: "", CreatedAt: time.Now()}, // pre-feature row
	}
	for i := range tasks {
		if err := db.Create(&tasks[i]).Error; err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	m, err := loadColdStartMetrics(db)
	if err != nil {
		t.Fatalf("loadColdStartMetrics: %v", err)
	}
	if m.Samples != 1 {
		t.Fatalf("samples = %d, want 1 (the unmeasured row must not count)", m.Samples)
	}
	if m.AvgMs != 400 {
		t.Errorf("avg_ms = %v, want 400", m.AvgMs)
	}
}

// The shared free fleet gets its own bucket even though it is, underneath,
// just another fleet id with no row in the fleets table — a super-admin
// cares about that capacity story separately from a customer's own fleet.
func TestLoadColdStartMetrics_FleetAndEcosystemBreakdown(t *testing.T) {
	db := newColdStartTestDB(t)
	if err := db.Create(&store.Fleet{ID: "flt-byoc-1", OrgID: "o2", Type: store.FleetBYOC}).Error; err != nil {
		t.Fatalf("create fleet: %v", err)
	}
	tasks := []store.QueuedTask{
		{ID: "t1", OrgID: "o1", FleetID: store.SharedFreeFleet, SandboxProvisionMs: 200, SandboxImage: "golang:1.25-alpine", CreatedAt: time.Now()},
		{ID: "t2", OrgID: "o1", FleetID: store.SharedFreeFleet, SandboxProvisionMs: 600, SandboxImage: "node:20-alpine", CreatedAt: time.Now()},
		{ID: "t3", OrgID: "o2", FleetID: "flt-byoc-1", SandboxProvisionMs: 100, SandboxImage: "python:3.12-slim", CreatedAt: time.Now()},
	}
	for i := range tasks {
		if err := db.Create(&tasks[i]).Error; err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	m, err := loadColdStartMetrics(db)
	if err != nil {
		t.Fatalf("loadColdStartMetrics: %v", err)
	}
	if m.Samples != 3 {
		t.Fatalf("samples = %d, want 3", m.Samples)
	}
	if got, want := m.AvgMs, 300.0; got != want {
		t.Errorf("avg_ms = %v, want %v", got, want)
	}

	fleetByLabel := map[string]coldStartPoint{}
	for _, p := range m.ByFleet {
		fleetByLabel[p.Label] = p
	}
	if p, ok := fleetByLabel["shared free fleet"]; !ok || p.Samples != 2 || p.AvgMs != 400 {
		t.Errorf("shared free fleet bucket = %+v, ok=%v, want 2 samples avg 400", p, ok)
	}
	if p, ok := fleetByLabel[store.FleetBYOC]; !ok || p.Samples != 1 || p.AvgMs != 100 {
		t.Errorf("byoc bucket = %+v, ok=%v, want 1 sample avg 100", p, ok)
	}

	ecoByLabel := map[string]coldStartPoint{}
	for _, p := range m.ByEco {
		ecoByLabel[p.Label] = p
	}
	for _, want := range []string{"golang", "node", "python"} {
		if _, ok := ecoByLabel[want]; !ok {
			t.Errorf("expected an ecosystem bucket for %q, got %+v", want, m.ByEco)
		}
	}
}

func TestLoadColdStartMetrics_NoSamplesIsZeroNotError(t *testing.T) {
	db := newColdStartTestDB(t)
	m, err := loadColdStartMetrics(db)
	if err != nil {
		t.Fatalf("loadColdStartMetrics: %v", err)
	}
	if m.Samples != 0 || m.AvgMs != 0 {
		t.Errorf("expected a zero-value result on an empty table, got %+v", m)
	}
}
