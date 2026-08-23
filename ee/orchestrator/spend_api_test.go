// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func day(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

func jobAt(id, repo, plannerModel string, created time.Time, plannerUSD float64) store.Job {
	return store.Job{
		ID:             id,
		CreatedAt:      created,
		PlannerCostUSD: plannerUSD,
		Inputs: map[string]interface{}{
			"repo_url":      "https://github.com/acme/" + repo,
			"planner_model": plannerModel,
		},
	}
}

func taskFor(jobID, model string, cost float64, metered *time.Time) store.QueuedTask {
	return store.QueuedTask{
		JobID:     jobID,
		CostUSD:   cost,
		MeteredAt: metered,
		Spec: map[string]interface{}{
			"model":    model,
			"repo_url": "https://github.com/acme/repo-a",
		},
	}
}

func TestBuildSpendEmptyRange(t *testing.T) {
	from, to := day("2026-07-01T00:00:00Z"), day("2026-07-03T00:00:00Z")
	got := buildSpend(from, to, nil, nil, "all")

	if got.CostUSD != 0 || got.JobCount != 0 || got.MeteredJobs != 0 {
		t.Errorf("expected an empty range to be all zeros, got %+v", got)
	}
	// Non-null so the client can iterate without a guard, and zero-filled so the
	// chart's axis spans the range the user asked for.
	if got.Daily == nil {
		t.Fatal("daily must be non-null even when empty")
	}
	if len(got.Daily) != 3 {
		t.Errorf("expected 3 zero-filled days, got %d", len(got.Daily))
	}
	if got.ByRepo == nil || got.ByModel == nil {
		t.Error("breakdowns must be non-null")
	}
}

// The distinction the whole page rests on: a job that ran and cost nothing is
// measured; a job from before metering shipped is not. Counting "cost > 0"
// would collapse them.
func TestMeteredJobsCountsCoverageNotSpend(t *testing.T) {
	from, to := day("2026-07-01T00:00:00Z"), day("2026-07-02T00:00:00Z")
	at := day("2026-07-01T10:00:00Z")

	jobs := []store.Job{
		jobAt("j-old", "repo-a", "claude-opus-4-8", at, 0),  // predates metering
		jobAt("j-free", "repo-a", "claude-opus-4-8", at, 0), // ran, cost nothing
	}
	tasks := []store.QueuedTask{
		taskFor("j-old", "m", 0, nil),      // never measured
		taskFor("j-free", "m", 0, ptr(at)), // measured, genuinely zero
	}

	got := buildSpend(from, to, jobs, tasks, "all")
	if got.JobCount != 2 {
		t.Fatalf("job_count: got %d, want 2", got.JobCount)
	}
	if got.MeteredJobs != 1 {
		t.Errorf("metered_jobs: got %d, want 1 — a zero-cost measured job is still measured", got.MeteredJobs)
	}
}

func TestPlannerAndWorkerSumToTotal(t *testing.T) {
	from, to := day("2026-07-01T00:00:00Z"), day("2026-07-02T00:00:00Z")
	at := day("2026-07-01T10:00:00Z")

	jobs := []store.Job{jobAt("j1", "repo-a", "claude-opus-4-8", at, 0.30)}
	tasks := []store.QueuedTask{taskFor("j1", "claude-haiku-4-5", 0.12, ptr(at))}

	got := buildSpend(from, to, jobs, tasks, "all")
	if got.PlannerUSD != 0.30 {
		t.Errorf("planner_usd: got %v, want 0.30", got.PlannerUSD)
	}
	if got.WorkerUSD != 0.12 {
		t.Errorf("worker_usd: got %v, want 0.12", got.WorkerUSD)
	}
	if diff := got.CostUSD - 0.42; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost_usd: got %v, want planner+worker = 0.42", got.CostUSD)
	}
}

// Planning is the half most worth acting on — it defaults to the most expensive
// model — so it must appear in the by-model breakdown, not just by-repo.
func TestByModelIncludesPlannerSpend(t *testing.T) {
	from, to := day("2026-07-01T00:00:00Z"), day("2026-07-02T00:00:00Z")
	at := day("2026-07-01T10:00:00Z")

	jobs := []store.Job{jobAt("j1", "repo-a", "claude-opus-4-8", at, 0.30)}
	tasks := []store.QueuedTask{taskFor("j1", "claude-haiku-4-5", 0.12, ptr(at))}

	got := buildSpend(from, to, jobs, tasks, "all")

	var opus, haiku *SpendBucket
	for i := range got.ByModel {
		switch got.ByModel[i].Label {
		case "claude-opus-4-8":
			opus = &got.ByModel[i]
		case "claude-haiku-4-5":
			haiku = &got.ByModel[i]
		}
	}
	if opus == nil {
		t.Fatal("the planner model is missing from by_model")
	}
	if opus.PlannerUSD != 0.30 || opus.WorkerUSD != 0 {
		t.Errorf("planner model bucket: got planner=%v worker=%v, want 0.30/0", opus.PlannerUSD, opus.WorkerUSD)
	}
	if haiku == nil || haiku.WorkerUSD != 0.12 {
		t.Errorf("worker model bucket missing or wrong: %+v", haiku)
	}
}

func TestDailyZeroFillAcrossMonthBoundary(t *testing.T) {
	from, to := day("2026-07-30T00:00:00Z"), day("2026-08-02T00:00:00Z")
	got := buildSpend(from, to, nil, nil, "all")

	want := []string{"2026-07-30", "2026-07-31", "2026-08-01", "2026-08-02"}
	if len(got.Daily) != len(want) {
		t.Fatalf("got %d days, want %d: %+v", len(got.Daily), len(want), got.Daily)
	}
	for i, w := range want {
		if got.Daily[i].Date != w {
			t.Errorf("day %d: got %s, want %s", i, got.Daily[i].Date, w)
		}
	}
}

// A row whose timestamp falls outside the generated span must be folded onto an
// edge, never dropped — silently losing spend from the chart is worse than
// attributing it to a boundary day.
func TestOutOfRangeRowIsFoldedNotDropped(t *testing.T) {
	from, to := day("2026-07-10T00:00:00Z"), day("2026-07-12T00:00:00Z")
	outside := day("2026-07-01T00:00:00Z")

	jobs := []store.Job{jobAt("j1", "repo-a", "m", outside, 0.25)}
	got := buildSpend(from, to, jobs, nil, "all")

	var total float64
	for _, p := range got.Daily {
		total += p.PlannerUSD
	}
	if total != 0.25 {
		t.Errorf("out-of-range spend was dropped from daily: summed %v, want 0.25", total)
	}
	if got.PlannerUSD != 0.25 {
		t.Errorf("planner_usd: got %v, want 0.25", got.PlannerUSD)
	}
}

func TestTailFoldsIntoOther(t *testing.T) {
	from, to := day("2026-07-01T00:00:00Z"), day("2026-07-02T00:00:00Z")
	at := day("2026-07-01T10:00:00Z")

	var jobs []store.Job
	// 8 distinct models, descending cost, so the folding boundary is unambiguous.
	for i := 0; i < 8; i++ {
		jobs = append(jobs, jobAt(
			string(rune('a'+i)), "repo-a",
			"model-"+string(rune('a'+i)),
			at, float64(10-i),
		))
	}

	got := buildSpend(from, to, jobs, nil, "all")
	if len(got.ByModel) != maxSpendBuckets+1 {
		t.Fatalf("expected %d buckets plus Other, got %d", maxSpendBuckets, len(got.ByModel))
	}
	last := got.ByModel[len(got.ByModel)-1]
	if last.Label != "Other" {
		t.Errorf("final bucket should be Other, got %q", last.Label)
	}
	// models g and h: 4 + 3
	if last.TotalUSD != 7 {
		t.Errorf("Other total: got %v, want 7", last.TotalUSD)
	}
}

func TestBreakdownOrderingIsStable(t *testing.T) {
	from, to := day("2026-07-01T00:00:00Z"), day("2026-07-02T00:00:00Z")
	at := day("2026-07-01T10:00:00Z")

	// Two models with identical spend must not swap between calls; a breakdown
	// that reshuffles as the page polls is unreadable.
	jobs := []store.Job{
		jobAt("j1", "repo-a", "model-b", at, 1),
		jobAt("j2", "repo-a", "model-a", at, 1),
	}
	first := buildSpend(from, to, jobs, nil, "all")
	second := buildSpend(from, to, jobs, nil, "all")

	if len(first.ByModel) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(first.ByModel))
	}
	for i := range first.ByModel {
		if first.ByModel[i].Label != second.ByModel[i].Label {
			t.Fatalf("ordering is unstable: %v vs %v", first.ByModel, second.ByModel)
		}
	}
	if first.ByModel[0].Label != "model-a" {
		t.Errorf("ties should break on label, got %q first", first.ByModel[0].Label)
	}
}

// cost_usd means "USD the org owes". Work Kiwi paid for must never be folded
// into it — showing a user a dollar total for work they were not billed for is
// the one thing this page must not do.
func TestBuildSpendExcludesKiwiFundedWorkFromCostUSD(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	jobs := []store.Job{
		{ID: "j1", OrgID: "o1", CreatedAt: metered, PlannerCostUSD: 3.00,
			Funding: store.FundingBYOK, Inputs: map[string]interface{}{"planner_model": "claude-opus-4-8"}},
		{ID: "j2", OrgID: "o1", CreatedAt: metered, PlannerCostUSD: 0.02,
			Funding: store.FundingKiwi, Inputs: map[string]interface{}{"planner_model": "kimi-k2"}},
	}
	tasks := []store.QueuedTask{
		{ID: "t1", JobID: "j1", CostUSD: 5.00, TokensIn: 100, TokensOut: 50,
			MeteredAt: &metered, Funding: store.FundingBYOK,
			Spec: map[string]interface{}{"model": "claude-opus-4-8"}},
		{ID: "t2", JobID: "j2", CostUSD: 0.04, TokensIn: 8000, TokensOut: 2000,
			MeteredAt: &metered, Funding: store.FundingKiwi,
			Spec: map[string]interface{}{"model": "kimi-k2"}},
	}

	got := buildSpend(from, to, jobs, tasks, "all")

	if got.CostUSD != 8.00 {
		t.Errorf("CostUSD = %v, want 8.00 (BYOK only: 3.00 planner + 5.00 worker)", got.CostUSD)
	}
	if got.PlannerUSD != 3.00 {
		t.Errorf("PlannerUSD = %v, want 3.00", got.PlannerUSD)
	}
	if got.WorkerUSD != 5.00 {
		t.Errorf("WorkerUSD = %v, want 5.00", got.WorkerUSD)
	}
	// Kiwi-funded usage is reported in tokens, on its own.
	if got.KiwiTokensIn != 8000 || got.KiwiTokensOut != 2000 {
		t.Errorf("Kiwi tokens = %d in / %d out, want 8000/2000", got.KiwiTokensIn, got.KiwiTokensOut)
	}
	// The BYOK token counts must not absorb the Kiwi ones.
	if got.TokensIn != 100 || got.TokensOut != 50 {
		t.Errorf("BYOK tokens = %d in / %d out, want 100/50", got.TokensIn, got.TokensOut)
	}
}

func TestBuildSpendFundingFilter(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	tasks := []store.QueuedTask{
		{ID: "t1", JobID: "j1", CostUSD: 5.00, MeteredAt: &metered,
			Funding: store.FundingBYOK, Spec: map[string]interface{}{"model": "claude-opus-4-8"}},
		{ID: "t2", JobID: "j2", TokensIn: 1000, MeteredAt: &metered,
			Funding: store.FundingKiwi, Spec: map[string]interface{}{"model": "kimi-k2"}},
	}

	byok := buildSpend(from, to, nil, tasks, store.FundingBYOK)
	for _, bucket := range byok.ByModel {
		if bucket.Label == "kimi-k2" {
			t.Error("a Kiwi-funded model appeared in a byok-filtered breakdown")
		}
	}

	kiwi := buildSpend(from, to, nil, tasks, store.FundingKiwi)
	for _, bucket := range kiwi.ByModel {
		if bucket.Label == "claude-opus-4-8" {
			t.Error("a BYOK model appeared in a kiwi-filtered breakdown")
		}
	}
}

func TestBuildSpendGroupsByProvider(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	tasks := []store.QueuedTask{
		{ID: "t1", JobID: "j1", CostUSD: 5.00, MeteredAt: &metered, Funding: store.FundingBYOK,
			Spec: map[string]interface{}{"model": "claude-opus-4-8"}},
		{ID: "t2", JobID: "j1", CostUSD: 2.00, MeteredAt: &metered, Funding: store.FundingBYOK,
			Spec: map[string]interface{}{"model": "claude-haiku-4-5-20251001"}},
		{ID: "t3", JobID: "j1", CostUSD: 1.00, MeteredAt: &metered, Funding: store.FundingBYOK,
			Spec: map[string]interface{}{"model": "gpt-5-mini"}},
	}

	got := buildSpend(from, to, nil, tasks, "all")

	byProvider := map[string]float64{}
	for _, b := range got.ByProvider {
		byProvider[b.Label] = b.TotalUSD
	}
	if byProvider["anthropic"] != 7.00 {
		t.Errorf("anthropic = %v, want 7.00", byProvider["anthropic"])
	}
	if byProvider["openai"] != 1.00 {
		t.Errorf("openai = %v, want 1.00", byProvider["openai"])
	}
}

// A zero-spend range and a never-measured range must stay distinguishable.
func TestBuildSpendPreservesMeteredJobsSemantics(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	tasks := []store.QueuedTask{
		{ID: "t1", JobID: "j1", MeteredAt: &metered, Funding: store.FundingBYOK},
		{ID: "t2", JobID: "j2", MeteredAt: nil, Funding: store.FundingBYOK},
	}
	got := buildSpend(from, to, nil, tasks, "all")
	if got.MeteredJobs != 1 {
		t.Errorf("MeteredJobs = %d, want 1", got.MeteredJobs)
	}
}

// JobCount and MeteredJobs are rendered together as a ratio ("X of Y jobs in
// this range carry cost data"). Counting the numerator after the funding filter
// and the denominator before it made a fully-metered org look entirely
// unmetered, showing the "metering was not deployed yet" empty state over jobs
// that were all metered.
func TestBuildSpendJobCountAndMeteredJobsUseTheSameFilter(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	jobs := []store.Job{
		{ID: "j1", OrgID: "o1", CreatedAt: metered, Funding: store.FundingKiwi},
		{ID: "j2", OrgID: "o1", CreatedAt: metered, Funding: store.FundingKiwi},
	}
	tasks := []store.QueuedTask{
		{ID: "t1", JobID: "j1", MeteredAt: &metered, Funding: store.FundingKiwi},
		{ID: "t2", JobID: "j2", MeteredAt: &metered, Funding: store.FundingKiwi},
	}

	kiwi := buildSpend(from, to, jobs, tasks, store.FundingKiwi)
	if kiwi.JobCount != 2 || kiwi.MeteredJobs != 2 {
		t.Errorf("funding=kiwi: JobCount=%d MeteredJobs=%d, want 2 and 2", kiwi.JobCount, kiwi.MeteredJobs)
	}

	// Filtering to BYOK leaves nothing at all, and must say so consistently
	// rather than claiming 2 jobs of which 0 were metered.
	byok := buildSpend(from, to, jobs, tasks, store.FundingBYOK)
	if byok.JobCount != 0 || byok.MeteredJobs != 0 {
		t.Errorf("funding=byok: JobCount=%d MeteredJobs=%d, want 0 and 0", byok.JobCount, byok.MeteredJobs)
	}
}

// A job and its tasks can have different funding: a Kiwi-tier planner model
// with a BYOK worker model, or the KIWI_PLANNER_API_KEY operator override. The
// task falls back to its job's repo when its own spec carries none, so a job
// dropped by the filter used to leave its BYOK worker dollars with an empty
// repo key — which bucket() discards, making the by-repo chart disagree with
// the headline by the full amount.
func TestBuildSpendKeepsRepoForTasksWhoseJobIsFilteredOut(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	jobs := []store.Job{{
		ID: "j1", OrgID: "o1", CreatedAt: metered, Funding: store.FundingKiwi,
		Inputs: map[string]interface{}{"repo_url": "https://github.com/acme/api"},
	}}
	tasks := []store.QueuedTask{{
		ID: "t1", JobID: "j1", CostUSD: 9.99, MeteredAt: &metered,
		Funding: store.FundingBYOK,
		Spec:    map[string]interface{}{"model": "claude-opus-4-8"},
	}}

	got := buildSpend(from, to, jobs, tasks, store.FundingBYOK)
	if got.CostUSD != 9.99 {
		t.Fatalf("CostUSD = %v, want 9.99", got.CostUSD)
	}
	var repoTotal float64
	for _, b := range got.ByRepo {
		repoTotal += b.TotalUSD
	}
	if repoTotal != got.CostUSD {
		t.Errorf("by_repo sums to %v but the headline says %v; the chart and the total disagree",
			repoTotal, got.CostUSD)
	}
}

// Kiwi-funded rows have their dollars zeroed. Bucketing them anyway renders a
// chart of $0.00 bars that reads as "these models were free" rather than "Kiwi
// paid for these", and the empty rows consume slots against maxSpendBuckets.
func TestBuildSpendOmitsZeroedKiwiRowsFromBreakdowns(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	jobs := []store.Job{{
		ID: "j1", OrgID: "o1", CreatedAt: metered, Funding: store.FundingKiwi,
		PlannerCostUSD: 0.02,
		Inputs: map[string]interface{}{
			"repo_url": "https://github.com/acme/kiwi", "planner_model": "kimi-k2",
		},
	}}
	tasks := []store.QueuedTask{{
		ID: "t1", JobID: "j1", CostUSD: 0.04, TokensIn: 8000, TokensOut: 2000,
		MeteredAt: &metered, Funding: store.FundingKiwi,
		Spec: map[string]interface{}{"model": "kimi-k2", "provider": "openrouter"},
	}}

	got := buildSpend(from, to, jobs, tasks, "all")
	for _, b := range got.ByModel {
		if b.Label == "kimi-k2" {
			t.Error("a Kiwi-funded model appears in the dollar breakdown as a $0.00 row")
		}
	}
	for _, b := range got.ByProvider {
		if b.Label == "anthropic" {
			t.Error("an OpenRouter model was filed under anthropic; the pinned provider was ignored")
		}
	}
	// Its usage is still reported — in tokens, where it belongs.
	if got.KiwiTokensIn != 8000 || got.KiwiTokensOut != 2000 {
		t.Errorf("Kiwi tokens = %d/%d, want 8000/2000", got.KiwiTokensIn, got.KiwiTokensOut)
	}
}

// Rows written before the funding column existed carry "". They are legacy
// BYOK and must survive a byok filter — the committed tests only ever exercised
// this through "all", where the include branch short-circuits.
func TestBuildSpendTreatsLegacyRowsAsBYOKUnderTheByokFilter(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	metered := from.AddDate(0, 0, 1)

	jobs := []store.Job{{ID: "j1", OrgID: "o1", CreatedAt: metered, PlannerCostUSD: 1.00, Funding: ""}}
	tasks := []store.QueuedTask{{
		ID: "t1", JobID: "j1", CostUSD: 0.50, MeteredAt: &metered, Funding: "",
		Spec: map[string]interface{}{"model": "claude-opus-4-8"},
	}}

	got := buildSpend(from, to, jobs, tasks, store.FundingBYOK)
	if got.CostUSD != 1.50 {
		t.Errorf("CostUSD = %v, want 1.50; pre-funding-column history vanished from the page", got.CostUSD)
	}
	if got.JobCount != 1 {
		t.Errorf("JobCount = %d, want 1", got.JobCount)
	}
}

// A brand-new org has no grant rows — they are created lazily on first use.
// The page must still show what the plan entitles them to; answering "no
// allowance" when the truth is "full allowance, none used" is the difference
// between a user knowing their quota and assuming they have none.
func TestSpendReportsFullAllowanceBeforeAnyUsage(t *testing.T) {
	s := newTestServer(t)
	seedFreeOrg(t, s, "o1")

	req := authed(http.MethodGet, "/api/v1/spend", "", "o1")
	rec := httptest.NewRecorder()
	s.handleSpend(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got SpendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Plan != "free" {
		t.Errorf("Plan = %q, want free", got.Plan)
	}
	if len(got.Allowance) == 0 {
		t.Fatal("no allowance reported for an org that has never run a task; the quota is invisible")
	}

	byTier := map[string]AllowanceBucket{}
	for _, a := range got.Allowance {
		byTier[a.Tier] = a
	}
	for _, tier := range []string{store.TierFree, store.TierEconomy, store.TierFrontier} {
		b, ok := byTier[tier]
		if !ok {
			t.Errorf("class %q missing from the allowance", tier)
			continue
		}
		if b.Granted <= 0 {
			t.Errorf("class %q granted = %d, want the plan's allowance", tier, b.Granted)
		}
		if b.Used != 0 {
			t.Errorf("class %q used = %d, want 0", tier, b.Used)
		}
		if b.Remaining != b.Granted {
			t.Errorf("class %q remaining = %d, want %d", tier, b.Remaining, b.Granted)
		}
	}
}

// Regression test: once a grant row exists, its own TokensGranted is what
// the page must report — not the plan's static default. A grant raised
// above the plan default (an operator top-up, applied directly against the
// row the same way entitlement.Checker.Allow reads it) used to be invisible
// here: this handler recomputed Granted from entitlement.PlanGrants(plan)
// every time, so a top-up that made real admission succeed again still
// showed as permanently exhausted on the dashboard.
func TestSpendAllowanceReflectsAGrantRaisedAboveThePlanDefault(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")

	period := store.CurrentPeriod(timeNow())
	// The free plan's economy default is 1,000,000 (see entitlement.PlanGrants).
	// Seed usage past that default, then top the grant up past usage — mirrors
	// an operator raising tokens_granted directly against an exhausted row.
	if _, err := s.storage.EnsureGrant(ctx, "o1", store.TierEconomy, period, 1_000_000); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	if err := s.storage.ConsumeTokens(ctx, "o1", store.TierEconomy, period, 1_259_665); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if err := s.db.WithContext(ctx).Model(&store.OrgTokenGrant{}).
		Where("org_id = ? AND tier = ? AND period = ?", "o1", store.TierEconomy, period).
		Update("tokens_granted", 5_000_000).Error; err != nil {
		t.Fatalf("top up grant: %v", err)
	}

	req := authed(http.MethodGet, "/api/v1/spend", "", "o1")
	rec := httptest.NewRecorder()
	s.handleSpend(rec, req)

	var got SpendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, a := range got.Allowance {
		if a.Tier != store.TierEconomy {
			continue
		}
		found = true
		if a.Granted != 5_000_000 {
			t.Errorf("granted = %d, want the row's own 5,000,000, not the plan's static 1,000,000 default", a.Granted)
		}
		if a.Remaining != 5_000_000-1_259_665 {
			t.Errorf("remaining = %d, want %d (the raised grant minus usage, not clamped to 0)", a.Remaining, 5_000_000-1_259_665)
		}
	}
	if !found {
		t.Fatal("economy allowance bucket missing from the response")
	}
}

// Once usage exists it must be reflected, and remaining must never go negative.
func TestSpendAllowanceReflectsUsage(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")

	period := store.CurrentPeriod(timeNow())
	if _, err := s.storage.EnsureGrant(ctx, "o1", store.TierEconomy, period, 1_000_000); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	// Overspend the bucket; remaining must clamp at zero, not report negative.
	if err := s.storage.ConsumeTokens(ctx, "o1", store.TierEconomy, period, 1_200_000); err != nil {
		t.Fatalf("consume: %v", err)
	}

	req := authed(http.MethodGet, "/api/v1/spend", "", "o1")
	rec := httptest.NewRecorder()
	s.handleSpend(rec, req)

	var got SpendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, a := range got.Allowance {
		if a.Tier != store.TierEconomy {
			continue
		}
		if a.Used != 1_200_000 {
			t.Errorf("used = %d, want 1200000", a.Used)
		}
		if a.Remaining != 0 {
			t.Errorf("remaining = %d, want 0 (clamped, never negative)", a.Remaining)
		}
	}
}

func TestHandleSpendIncludesQuotaCeilings(t *testing.T) {
	s := newTestServer(t)
	seedFreeOrg(t, s, "org-quota-test")

	if err := s.db.Create(&store.OrgLimits{
		OrgID:                   "org-quota-test",
		MaxAgentMinutesPerMonth: 750.0,
		MaxConcurrentJobs:       12,
	}).Error; err != nil {
		t.Fatalf("seed limits: %v", err)
	}

	now := time.Now().UTC()
	if err := s.db.Create(&store.QueuedTask{
		ID: "t1", OrgID: "org-quota-test", JobID: "j1", Status: store.TaskLeased, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed task t1: %v", err)
	}
	if err := s.db.Create(&store.QueuedTask{
		ID: "t2", OrgID: "org-quota-test", JobID: "j2", Status: store.TaskLeased, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed task t2: %v", err)
	}
	if err := s.db.Create(&store.QueuedTask{
		ID: "t3", OrgID: "org-quota-test", JobID: "j3", Status: store.TaskSucceeded, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed task t3: %v", err)
	}
	if err := s.db.Create(&store.QueuedTask{
		ID: "t4", OrgID: "other-org", JobID: "j4", Status: store.TaskLeased, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed task t4: %v", err)
	}

	req := authed(http.MethodGet, "/api/v1/spend", "", "org-quota-test")
	rec := httptest.NewRecorder()
	s.handleSpend(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got SpendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.AgentMinutesLimit != 750.0 {
		t.Errorf("AgentMinutesLimit = %v, want 750.0", got.AgentMinutesLimit)
	}
	if got.ConcurrentLeasesMax != 12 {
		t.Errorf("ConcurrentLeasesMax = %d, want 12", got.ConcurrentLeasesMax)
	}
	if got.ConcurrentLeasesActive != 2 {
		t.Errorf("ConcurrentLeasesActive = %d, want 2", got.ConcurrentLeasesActive)
	}
}
