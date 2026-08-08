// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// maxSpendRange bounds a query so a hand-edited URL cannot ask for an unbounded
// scan. A year is well past any reporting window the dashboard offers.
const maxSpendRange = 365 * 24 * time.Hour

// spendBuckets returned per breakdown before the tail is folded into "Other".
const maxSpendBuckets = 6

type SpendBucket struct {
	Label      string  `json:"label"`
	PlannerUSD float64 `json:"planner_usd"`
	WorkerUSD  float64 `json:"worker_usd"`
	TotalUSD   float64 `json:"total_usd"`
}

type SpendPoint struct {
	Date       string  `json:"date"`
	PlannerUSD float64 `json:"planner_usd"`
	WorkerUSD  float64 `json:"worker_usd"`
}

type SpendResponse struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`

	CostUSD    float64 `json:"cost_usd"`
	PlannerUSD float64 `json:"planner_usd"`
	WorkerUSD  float64 `json:"worker_usd"`

	AgentMinutes float64 `json:"agent_minutes"`
	TokensIn     int64   `json:"tokens_in"`
	TokensOut    int64   `json:"tokens_out"`

	// Kiwi-funded usage is reported in tokens and never in dollars. Folding it
	// into CostUSD would show the org a bill for work it was not charged for;
	// omitting it entirely would make its usage invisible. Two ledgers, one page.
	KiwiTokensIn  int64 `json:"kiwi_tokens_in"`
	KiwiTokensOut int64 `json:"kiwi_tokens_out"`

	JobCount int `json:"job_count"`
	// MeteredJobs counts jobs with at least one task carrying metered_at. It is
	// what lets the caller tell "spent nothing" from "never measured": jobs that
	// ran before cost metering shipped have no metered task at all, and their
	// zero must not be presented as a measurement.
	MeteredJobs int `json:"metered_jobs"`

	Daily      []SpendPoint      `json:"daily"`
	ByRepo     []SpendBucket     `json:"by_repo"`
	ByModel    []SpendBucket     `json:"by_model"`
	ByProvider []SpendBucket     `json:"by_provider"`
	Allowance  []AllowanceBucket `json:"allowance"`
}

// AllowanceBucket is one tier's Kiwi token allowance for the current period.
// Remaining is -1 when the grant is unlimited.
type AllowanceBucket struct {
	Tier      string `json:"tier"`
	Period    string `json:"period"`
	Granted   int64  `json:"granted"`
	Used      int64  `json:"used"`
	Remaining int64  `json:"remaining"`
}

// handleSpend serves GET /api/v1/spend?from=&to=. The org comes from the
// authenticated claims and never from a parameter.
func (s *Server) handleSpend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	to := parseTimeOr(r.URL.Query().Get("to"), time.Now().UTC())
	from := parseTimeOr(r.URL.Query().Get("from"), to.Add(-30*24*time.Hour))
	if to.Before(from) {
		from, to = to, from
	}
	if to.Sub(from) > maxSpendRange {
		from = to.Add(-maxSpendRange)
	}

	funding := r.URL.Query().Get("funding")
	switch funding {
	case "", "all", store.FundingBYOK, store.FundingKiwi:
	default:
		http.Error(w, "funding must be byok, kiwi, or all", http.StatusBadRequest)
		return
	}

	var jobs []store.Job
	if err := s.db.WithContext(r.Context()).
		Where("org_id = ? AND created_at >= ? AND created_at <= ?", claims.OrgID, from, to).
		Find(&jobs).Error; err != nil {
		http.Error(w, "failed to query jobs", http.StatusInternalServerError)
		return
	}

	var tasks []store.QueuedTask
	if err := s.db.WithContext(r.Context()).
		Joins("JOIN jobs ON jobs.id = queued_tasks.job_id").
		Where("jobs.org_id = ? AND jobs.created_at >= ? AND jobs.created_at <= ?", claims.OrgID, from, to).
		Find(&tasks).Error; err != nil {
		http.Error(w, "failed to query tasks", http.StatusInternalServerError)
		return
	}

	resp := buildSpend(from, to, jobs, tasks, funding)

	// The allowance is always the *current* period's, regardless of the range
	// being reported: it is a live balance, not a historical total, and dating
	// it to an arbitrary window would make it read as one.
	period := store.CurrentPeriod(time.Now())
	grants, gerr := s.storage.ListGrants(r.Context(), claims.OrgID, period)
	if gerr != nil {
		// Degrade to an empty allowance panel rather than failing the page, but
		// say so: an empty panel and a broken query look identical otherwise.
		log.Printf("[spend] loading grants for org %s: %v", claims.OrgID, gerr)
	} else {
		for _, g := range grants {
			resp.Allowance = append(resp.Allowance, AllowanceBucket{
				Tier: g.Tier, Period: g.Period,
				Granted: g.TokensGranted, Used: g.TokensUsed, Remaining: g.Remaining(),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseTimeOr(v string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC()
	}
	return fallback.UTC()
}

// buildSpend aggregates a range. Split out from the handler so the arithmetic —
// which is where the honesty lives — is testable without a database.
//
// funding is "byok", "kiwi", or "all". CostUSD, PlannerUSD and WorkerUSD always
// count BYOK rows only, whatever the filter: they mean "USD the org owes", and
// a Kiwi-funded row was never owed.
func buildSpend(from, to time.Time, jobs []store.Job, tasks []store.QueuedTask, funding string) SpendResponse {
	resp := SpendResponse{
		From:       from,
		To:         to,
		Daily:      make([]SpendPoint, 0),
		ByRepo:     make([]SpendBucket, 0),
		ByModel:    make([]SpendBucket, 0),
		ByProvider: make([]SpendBucket, 0),
		Allowance:  make([]AllowanceBucket, 0),
	}

	byProvider := map[string]*spendAgg{}

	// include reports whether a row passes the caller's funding filter.
	include := func(rowFunding string) bool {
		if rowFunding == "" {
			rowFunding = store.FundingBYOK // rows written before funding existed
		}
		return funding == "" || funding == "all" || funding == rowFunding
	}
	isBYOK := func(rowFunding string) bool {
		return rowFunding == "" || rowFunding == store.FundingBYOK
	}

	// Zero-fill every day in the range. A series that omits empty days rescales
	// its own axis and implies work happened on days that had none. Keys are
	// derived in UTC on both sides so a job can never land on a day that was
	// never created and get dropped from the chart.
	daily := map[string]*SpendPoint{}
	startDay := from.UTC().Truncate(24 * time.Hour)
	endDay := to.UTC().Truncate(24 * time.Hour)
	for d := startDay; !d.After(endDay); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		daily[key] = &SpendPoint{Date: key}
	}
	// Anything outside the generated span (clock skew, a boundary row) is folded
	// onto the nearest edge rather than silently discarded.
	pointFor := func(t time.Time) *SpendPoint {
		key := t.UTC().Format("2006-01-02")
		if p, ok := daily[key]; ok {
			return p
		}
		if t.Before(startDay) {
			return daily[startDay.Format("2006-01-02")]
		}
		return daily[endDay.Format("2006-01-02")]
	}

	byRepo := map[string]*spendAgg{}
	byModel := map[string]*spendAgg{}
	bucket := func(m map[string]*spendAgg, k string) *spendAgg {
		if k == "" {
			return nil
		}
		if _, ok := m[k]; !ok {
			m[k] = &spendAgg{}
		}
		return m[k]
	}

	// Jobs carry planner spend and agent-minutes.
	jobRepo := map[string]string{}
	for _, j := range jobs {
		// Populated for every job, filtered or not. A task falls back to its
		// job's repo when its own spec carries none, and a job excluded by the
		// funding filter would otherwise leave its BYOK tasks with an empty repo
		// key — which bucket() drops, silently losing real dollars from the
		// by-repo breakdown while they stay in the headline total.
		repo := ""
		if j.Inputs != nil {
			if u, ok := j.Inputs["repo_url"].(string); ok {
				repo = store.ShortRepo(u)
			}
		}
		jobRepo[j.ID] = repo

		if !include(j.Funding) {
			continue
		}
		// Counted after the filter, so JobCount and MeteredJobs always describe
		// the same set of jobs. The frontend renders them as a ratio ("X of Y
		// jobs carry cost data"); an unfiltered numerator over a filtered
		// denominator made a fully-metered org look entirely unmetered.
		resp.JobCount++

		plannerCostUSD := j.PlannerCostUSD
		if isBYOK(j.Funding) {
			resp.PlannerUSD += j.PlannerCostUSD
			resp.TokensIn += j.PlannerTokensIn
			resp.TokensOut += j.PlannerTokensOut
		} else {
			resp.KiwiTokensIn += j.PlannerTokensIn
			resp.KiwiTokensOut += j.PlannerTokensOut
			plannerCostUSD = 0 // never leak Kiwi cost to user
		}

		resp.AgentMinutes += j.AgentMinutes

		if p := pointFor(j.CreatedAt); p != nil {
			p.PlannerUSD += plannerCostUSD
		}

		if isBYOK(j.Funding) {
			if b := bucket(byRepo, repo); b != nil {
				b.planner += plannerCostUSD
			}
		}
		// Planner spend belongs in the by-model breakdown too. Omitting it hides
		// the most actionable fact on the page: the planner defaults to the most
		// expensive model while workers default to the cheapest.
		if j.Inputs != nil && isBYOK(j.Funding) {
			if pm, ok := j.Inputs["planner_model"].(string); ok {
				if b := bucket(byModel, pm); b != nil {
					b.planner += plannerCostUSD
				}
				if b := bucket(byProvider, jobProvider(j, pm)); b != nil {
					b.planner += plannerCostUSD
				}
			}
		}
	}

	// Tasks carry worker spend, and metered_at is the coverage signal.
	meteredJobs := map[string]bool{}
	for _, t := range tasks {
		if !include(t.Funding) {
			continue
		}
		if t.MeteredAt != nil {
			meteredJobs[t.JobID] = true
		}

		workerCostUSD := t.CostUSD
		if isBYOK(t.Funding) {
			resp.WorkerUSD += t.CostUSD
			resp.TokensIn += t.TokensIn
			resp.TokensOut += t.TokensOut
		} else {
			resp.KiwiTokensIn += t.TokensIn
			resp.KiwiTokensOut += t.TokensOut
			workerCostUSD = 0
		}

		if t.MeteredAt != nil {
			if p := pointFor(*t.MeteredAt); p != nil {
				p.WorkerUSD += workerCostUSD
			}
		}

		if !isBYOK(t.Funding) {
			// Kiwi-funded work has no dollars to break down; its ledger is the
			// token allowance. Bucketing it anyway renders $0.00 bars that read
			// as "this model was free" and crowds real rows out of the fold cap.
			continue
		}

		repo := specString(t.Spec, "repo_url")
		key := store.ShortRepo(repo)
		if key == "" {
			key = jobRepo[t.JobID]
		}
		if b := bucket(byRepo, key); b != nil {
			b.worker += workerCostUSD
		}
		model := specString(t.Spec, "model")
		if b := bucket(byModel, model); b != nil {
			b.worker += workerCostUSD
		}
		// The provider the planner resolved through the catalog, pinned on the
		// spec. ProviderOf is prefix inference and returns "anthropic" for
		// anything it does not recognise, which would file every aggregator
		// model under a provider that never served it.
		if b := bucket(byProvider, taskProvider(t, model)); b != nil {
			b.worker += workerCostUSD
		}
	}
	resp.MeteredJobs = len(meteredJobs)
	resp.CostUSD = resp.PlannerUSD + resp.WorkerUSD

	for _, p := range daily {
		resp.Daily = append(resp.Daily, *p)
	}
	sort.Slice(resp.Daily, func(i, j int) bool { return resp.Daily[i].Date < resp.Daily[j].Date })

	resp.ByRepo = foldBuckets(byRepo)
	resp.ByModel = foldBuckets(byModel)
	resp.ByProvider = foldBuckets(byProvider)
	return resp
}

// spendAgg accumulates one breakdown row while the range is walked.
type spendAgg struct{ planner, worker float64 }

// foldBuckets sorts by total spend and folds everything past the cap into a
// single "Other", because a breakdown with more classes than the chart can
// colour is a table, not a chart.
func foldBuckets(m map[string]*spendAgg) []SpendBucket {
	out := make([]SpendBucket, 0, len(m))
	for label, a := range m {
		out = append(out, SpendBucket{
			Label:      label,
			PlannerUSD: a.planner,
			WorkerUSD:  a.worker,
			TotalUSD:   a.planner + a.worker,
		})
	}
	// Ties break on label so identical input always renders identically; a
	// breakdown that reshuffles between polls is unreadable.
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalUSD != out[j].TotalUSD {
			return out[i].TotalUSD > out[j].TotalUSD
		}
		return out[i].Label < out[j].Label
	})
	if len(out) <= maxSpendBuckets {
		return out
	}
	var other SpendBucket
	other.Label = "Other"
	for _, b := range out[maxSpendBuckets:] {
		other.PlannerUSD += b.PlannerUSD
		other.WorkerUSD += b.WorkerUSD
		other.TotalUSD += b.TotalUSD
	}
	return append(out[:maxSpendBuckets:maxSpendBuckets], other)
}

// taskProvider reads the provider the Control Plane resolved for a task,
// falling back to inference for tasks queued before it was recorded.
func taskProvider(t store.QueuedTask, model string) string {
	if p := specString(t.Spec, "provider"); p != "" {
		return p
	}
	return provider.ProviderOf(model)
}

// jobProvider does the same for a job's planner model.
func jobProvider(j store.Job, plannerModel string) string {
	if j.Inputs != nil {
		if p, ok := j.Inputs["planner_provider"].(string); ok && p != "" {
			return p
		}
	}
	return provider.ProviderOf(plannerModel)
}
