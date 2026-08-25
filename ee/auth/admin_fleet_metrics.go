// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"sort"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// coldStartWindow bounds how far back the trend and breakdowns look. 30 days
// matches the other admin/analytics endpoints (see caching_analytics_api.go)
// and is enough to see whether provisioning is regressing without scanning
// the platform's whole task history on every fleet-tab load.
const coldStartWindow = 30 * 24 * time.Hour

// coldStartPoint is one bucket of the average-cold-start breakdown, shaped
// identically whether the bucket is a calendar day, a fleet type, or an
// ecosystem — the frontend renders all three the same way.
type coldStartPoint struct {
	Label   string  `json:"label"`
	AvgMs   float64 `json:"avg_ms"`
	Samples int64   `json:"samples"`
}

// coldStartSample is the sliver of a queued_tasks row this aggregation
// needs — selected explicitly rather than fetching full rows, since a
// platform-wide 30-day scan is the widest query on this page.
type coldStartSample struct {
	CreatedAt          time.Time
	SandboxProvisionMs int64
	SandboxImage       string
	FleetID            string
}

// coldStartMetrics is folded once and used for the single average plus all
// three breakdowns, so the 30-day scan happens exactly once per request.
type coldStartMetrics struct {
	AvgMs   float64          `json:"avg_ms"`
	Samples int64            `json:"samples"`
	Daily   []coldStartPoint `json:"daily"`
	ByFleet []coldStartPoint `json:"by_fleet_type"`
	ByEco   []coldStartPoint `json:"by_ecosystem"`
}

// coldStartAcc accumulates one bucket's running sum/count while folding.
type coldStartAcc struct {
	sumMs float64
	n     int64
}

// loadColdStartMetrics scans queued_tasks for the window and folds every
// breakdown in Go, matching the rest of this codebase's admin/analytics
// endpoints (see spend_api.go, caching_analytics_api.go) rather than
// introducing SQL-side GROUP BY aggregation as a new pattern.
//
// Only rows with SandboxProvisionMs > 0 count: a task that predates this
// column, or whose sandbox measurement genuinely couldn't be confirmed
// (see watchProvisioning), defaults to 0 — including it would silently drag
// the average toward a number nothing actually measured.
func loadColdStartMetrics(db *gorm.DB) (coldStartMetrics, error) {
	var samples []coldStartSample
	err := db.Model(&store.QueuedTask{}).
		Select("created_at", "sandbox_provision_ms", "sandbox_image", "fleet_id").
		Where("sandbox_provision_ms > 0 AND created_at >= ?", time.Now().Add(-coldStartWindow)).
		Find(&samples).Error
	if err != nil {
		return coldStartMetrics{}, err
	}
	if len(samples) == 0 {
		return coldStartMetrics{}, nil
	}

	var fleets []store.Fleet
	if err := db.Find(&fleets).Error; err != nil {
		return coldStartMetrics{}, err
	}
	fleetType := make(map[string]string, len(fleets))
	for _, f := range fleets {
		fleetType[f.ID] = f.Type
	}

	byDay := map[string]*coldStartAcc{}
	byFleet := map[string]*coldStartAcc{}
	byEco := map[string]*coldStartAcc{}

	var totalMs float64
	for _, s := range samples {
		ms := float64(s.SandboxProvisionMs)
		totalMs += ms

		coldStartBucketAdd(byDay, s.CreatedAt.UTC().Format("2006-01-02"), ms)
		coldStartBucketAdd(byFleet, fleetLabel(s.FleetID, fleetType), ms)

		eco, _, _ := strings.Cut(s.SandboxImage, ":")
		if eco == "" {
			eco = "unknown"
		}
		coldStartBucketAdd(byEco, eco, ms)
	}

	return coldStartMetrics{
		AvgMs:   totalMs / float64(len(samples)),
		Samples: int64(len(samples)),
		Daily:   sortedColdStartPoints(byDay),
		ByFleet: sortedColdStartPoints(byFleet),
		ByEco:   sortedColdStartPoints(byEco),
	}, nil
}

// fleetLabel names a fleet type for the breakdown, distinguishing the shared
// free fleet from an org's own managed or BYOC fleet even though both are
// technically store.FleetManaged rows — the shared fleet is a different
// capacity story for a super-admin than a customer's own dedicated box.
func fleetLabel(fleetID string, fleetType map[string]string) string {
	if fleetID == store.SharedFreeFleet {
		return "shared free fleet"
	}
	if t, ok := fleetType[fleetID]; ok {
		return t
	}
	return "unknown"
}

func coldStartBucketAdd(m map[string]*coldStartAcc, key string, ms float64) {
	b, ok := m[key]
	if !ok {
		b = &coldStartAcc{}
		m[key] = b
	}
	b.sumMs += ms
	b.n++
}

func sortedColdStartPoints(m map[string]*coldStartAcc) []coldStartPoint {
	out := make([]coldStartPoint, 0, len(m))
	for label, b := range m {
		out = append(out, coldStartPoint{Label: label, AvgMs: b.sumMs / float64(b.n), Samples: b.n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
