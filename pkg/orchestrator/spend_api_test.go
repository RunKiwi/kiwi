package orchestrator

import (
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
