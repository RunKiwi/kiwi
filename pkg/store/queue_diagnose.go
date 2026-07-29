package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Blocked reason codes explaining why a task is sitting in QUEUED. A QUEUED row
// on its own says only "nobody has leased this yet" — which is the same display
// whether the work is about to start, the org has no runner at all, or a cap is
// silently refusing every lease. These codes are what let a caller tell those
// apart. They are stable identifiers: the UI may branch on them, so treat a
// rename as a breaking change.
const (
	// BlockNone means the task is not blocked — it is leased, terminal, or
	// otherwise has nothing to explain.
	BlockNone = ""
	// BlockAwaitingRunner is the healthy case: a live daemon serves this task's
	// fleet and simply has not picked it up yet. Normal for a few seconds.
	BlockAwaitingRunner = "awaiting_runner"
	// BlockProvisioning means a free-tier daemon is being cold-started for the org.
	BlockProvisioning = "provisioning"
	// BlockProvisionFailed means the last provisioning attempt failed outright.
	BlockProvisionFailed = "provision_failed"
	// BlockNoRunner means no daemon is registered that could ever lease this task.
	BlockNoRunner = "no_runner"
	// BlockRunnerOffline means daemons are registered for the fleet but none has
	// heartbeated recently, so nothing is polling for work.
	BlockRunnerOffline = "runner_offline"
	// BlockConcurrencyCap means the org is at MaxConcurrentJobs.
	BlockConcurrencyCap = "concurrency_cap"
	// BlockComputeCap means the org is at MaxAgentMinutesPerMonth.
	BlockComputeCap = "compute_cap"
	// BlockWaitingDependencies means the task's DAG predecessors are unfinished.
	BlockWaitingDependencies = "waiting_on_dependencies"
)

// daemonStaleAfter is how long without a heartbeat before a registered daemon is
// reported offline. The daemon polls on a 5s base interval that backs off to a
// 60s ceiling (plus jitter) on failure, so a live-but-struggling daemon still
// touches its row about once a minute. Three minutes therefore accuses only a
// daemon that is genuinely gone, not one that is merely backing off.
const daemonStaleAfter = 3 * time.Minute

// TaskDiagnosis explains why one task has not started. Reason is a stable code
// for programmatic branching; Detail is the human-readable sentence to show.
type TaskDiagnosis struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// effectiveOrgLimits loads an org's limits, falling back to the platform
// defaults when the org has no row. LeaseNextTask and DiagnoseQueuedTasks both
// use it so the explanation cannot drift from the enforcement it describes.
func effectiveOrgLimits(tx *gorm.DB, orgID string) (OrgLimits, error) {
	var limits OrgLimits
	if err := tx.First(&limits, "org_id = ?", orgID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return limits, err
		}
		limits = OrgLimits{
			OrgID:              orgID,
			MaxConcurrentJobs:  10,
			MaxBudgetPerJob:    5.00,
			MaxBudgetPerMonth:  500.00,
			MaxWorkersPerJob:   8,
			TaskTimeoutSeconds: 1800,
			MaxSandboxDiskMB:   2048,
		}
	}
	return limits, nil
}

// countLeased returns how many of an org's tasks are currently LEASED — the
// in-flight count the concurrency cap is checked against.
func countLeased(tx *gorm.DB, orgID string) (int64, error) {
	var n int64
	err := tx.Model(&QueuedTask{}).Where("org_id = ? AND status = ?", orgID, TaskLeased).Count(&n).Error
	return n, err
}

// agentMinutesThisMonth sums an org's metered agent-minutes since the start of
// the current UTC month — the usage the compute cap is checked against.
func agentMinutesThisMonth(tx *gorm.DB, orgID string) (float64, error) {
	var used float64
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	err := tx.Model(&Job{}).
		Where("org_id = ? AND created_at >= ?", orgID, monthStart).
		Select("COALESCE(SUM(agent_minutes), 0)").
		Scan(&used).Error
	return used, err
}

// provisionState is the org's most recent free-tier cold-start attempt, used to
// distinguish "a runner is on its way" from "the runner never came".
type provisionState struct {
	pending    bool   // a request is pending or in_progress
	failed     bool   // the most recent request failed and none is pending
	failedErr  string // that request's recorded error, if any
	pendingAge time.Duration
}

// DiagnoseQueuedTasks explains, for each QUEUED task in tasks, why it has not
// started yet. Tasks in any other state are omitted from the result.
//
// It is deliberately a batch call taking the tasks the caller already has: a
// job's tasks are diagnosed with a fixed handful of queries regardless of how
// many there are, so the job-status endpoint stays cheap enough to poll. It is a
// pure read — nothing here mutates state or affects scheduling.
//
// The checks run in the order an operator would: the DAG first (intrinsic to the
// plan), then whether a runner exists at all (the infrastructure answer, and the
// actionable one), then the caps (which only bite once a runner is actually
// polling). Note the orderings rarely conflict in practice — being at the
// concurrency cap implies sibling tasks are running, which implies a live runner.
func (s *PostgresStore) DiagnoseQueuedTasks(ctx context.Context, orgID string, tasks []QueuedTask) (map[string]TaskDiagnosis, error) {
	out := make(map[string]TaskDiagnosis)

	var queued []QueuedTask
	for i := range tasks {
		if tasks[i].Status == TaskQueued {
			queued = append(queued, tasks[i])
		}
	}
	if len(queued) == 0 {
		return out, nil
	}

	db := s.db.WithContext(ctx)

	depStatuses, err := dependencyStatuses(db, orgID, queued)
	if err != nil {
		return nil, err
	}

	daemons, err := s.ListDaemons(ctx, orgID)
	if err != nil {
		return nil, err
	}

	prov := latestProvisionState(db, orgID)

	limits, err := effectiveOrgLimits(db, orgID)
	if err != nil {
		return nil, err
	}
	inFlight, err := countLeased(db, orgID)
	if err != nil {
		return nil, err
	}
	var usedMinutes float64
	if limits.MaxAgentMinutesPerMonth > 0 {
		if usedMinutes, err = agentMinutesThisMonth(db, orgID); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	for i := range queued {
		t := &queued[i]

		if unmet := unmetDependencies(t, depStatuses); len(unmet) > 0 {
			out[t.ID] = TaskDiagnosis{
				Reason: BlockWaitingDependencies,
				Detail: fmt.Sprintf("waiting on %s to finish first", strings.Join(unmet, ", ")),
			}
			continue
		}

		if d := diagnoseRunner(t.FleetID, daemons, prov, now); d.Reason != BlockAwaitingRunner {
			out[t.ID] = d
			continue
		}

		if inFlight >= int64(limits.MaxConcurrentJobs) {
			out[t.ID] = TaskDiagnosis{
				Reason: BlockConcurrencyCap,
				Detail: fmt.Sprintf("your plan runs %d tasks at once and %d are already running", limits.MaxConcurrentJobs, inFlight),
			}
			continue
		}

		if limits.MaxAgentMinutesPerMonth > 0 && usedMinutes >= limits.MaxAgentMinutesPerMonth {
			out[t.ID] = TaskDiagnosis{
				Reason: BlockComputeCap,
				Detail: fmt.Sprintf("this month's agent-minute allowance is used up (%.0f of %.0f)", usedMinutes, limits.MaxAgentMinutesPerMonth),
			}
			continue
		}

		out[t.ID] = TaskDiagnosis{
			Reason: BlockAwaitingRunner,
			Detail: "waiting for a runner to pick this up",
		}
	}

	return out, nil
}

// diagnoseRunner reports whether any registered daemon could lease a task on
// fleetID, mirroring the routing rule in LeaseNextTask: a daemon on fleet F
// serves tasks pinned to F plus unassigned tasks (fleet_id = ""). It returns
// BlockAwaitingRunner when a live daemon does serve the fleet — the caller then
// moves on to the cap checks.
func diagnoseRunner(fleetID string, daemons []Daemon, prov provisionState, now time.Time) TaskDiagnosis {
	var serving, live int
	for i := range daemons {
		d := &daemons[i]
		if fleetID != "" && d.FleetID != fleetID {
			continue
		}
		serving++
		if d.LastSeenAt != nil && now.Sub(*d.LastSeenAt) <= daemonStaleAfter {
			live++
		}
	}

	if live > 0 {
		return TaskDiagnosis{Reason: BlockAwaitingRunner}
	}

	// No live runner. A cold-start in flight is the benign explanation and takes
	// precedence: the runner is coming, the user just has to wait for it.
	if prov.pending {
		return TaskDiagnosis{
			Reason: BlockProvisioning,
			Detail: fmt.Sprintf("starting a runner for your org (%s so far)", roundedAge(prov.pendingAge)),
		}
	}
	if prov.failed {
		detail := "could not start a runner for your org"
		if prov.failedErr != "" {
			detail += ": " + prov.failedErr
		}
		return TaskDiagnosis{Reason: BlockProvisionFailed, Detail: detail}
	}

	if serving > 0 {
		return TaskDiagnosis{
			Reason: BlockRunnerOffline,
			Detail: fmt.Sprintf("%s registered but not responding — no heartbeat in over %s", pluralDaemons(serving), roundedAge(daemonStaleAfter)),
		}
	}
	return TaskDiagnosis{
		Reason: BlockNoRunner,
		Detail: "no runner is connected that can execute this task",
	}
}

func pluralDaemons(n int) string {
	if n == 1 {
		return "1 runner is"
	}
	return fmt.Sprintf("%d runners are", n)
}

// roundedAge renders a duration for humans: "45s", "4m", "2h".
func roundedAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Round(time.Second).Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute).Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Round(time.Hour).Hours()))
	}
}

// latestProvisionState summarises an org's most recent free-tier provisioning
// request. A pending/in_progress request means a runner is on its way; a failed
// most-recent request (with nothing pending behind it) means the cold-start is
// not going to happen on its own, because a failed request is terminal and only
// a fresh submit enqueues another.
//
// A query failure here is swallowed rather than propagated: provisioning is an
// optional refinement of the runner diagnosis (it turns "no runner" into "a
// runner is starting"), and a BYOC deployment that never provisions may not even
// have the table. Losing the refinement is acceptable; losing the whole
// explanation because of it is not.
func latestProvisionState(db *gorm.DB, orgID string) provisionState {
	var reqs []struct {
		Status    string
		Error     string
		CreatedAt time.Time
	}
	// provisioning_requests is owned by pkg/auth; querying it by table name keeps
	// store from importing auth (which imports store).
	if err := db.Table("provisioning_requests").
		Select("status", "error", "created_at").
		Where("org_id = ? AND type = ?", orgID, "provision").
		Order("created_at DESC").
		Limit(5).
		Scan(&reqs).Error; err != nil {
		return provisionState{}
	}

	var st provisionState
	for _, r := range reqs {
		if r.Status == "pending" || r.Status == "in_progress" {
			st.pending = true
			st.pendingAge = time.Since(r.CreatedAt)
			return st
		}
	}
	if len(reqs) > 0 && reqs[0].Status == "failed" {
		st.failed = true
		st.failedErr = reqs[0].Error
	}
	return st
}

// dependencyStatuses resolves the statuses of every dependency referenced by the
// given tasks in a single query, keyed by sibling task id.
func dependencyStatuses(db *gorm.DB, orgID string, tasks []QueuedTask) (map[string]string, error) {
	depSet := make(map[string]struct{})
	for i := range tasks {
		for _, id := range dependencyTaskIDs(&tasks[i]) {
			depSet[id] = struct{}{}
		}
	}
	statuses := make(map[string]string, len(depSet))
	if len(depSet) == 0 {
		return statuses, nil
	}

	ids := make([]string, 0, len(depSet))
	for id := range depSet {
		ids = append(ids, id)
	}
	var deps []QueuedTask
	if err := db.Select("id", "status").
		Where("org_id = ? AND id IN ?", orgID, ids).
		Find(&deps).Error; err != nil {
		return nil, err
	}
	for _, d := range deps {
		statuses[d.ID] = d.Status
	}
	return statuses, nil
}

// unmetDependencies lists the worker IDs of t's dependencies that have not yet
// SUCCEEDED, in stable order. A dependency with no row at all counts as unmet,
// matching dependenciesSatisfied.
func unmetDependencies(t *QueuedTask, statuses map[string]string) []string {
	var unmet []string
	for _, id := range dependencyTaskIDs(t) {
		if statuses[id] != TaskSucceeded {
			unmet = append(unmet, strings.TrimPrefix(id, t.JobID+"-"))
		}
	}
	sort.Strings(unmet)
	return unmet
}
