package planner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/entitlement"
	"github.com/ibreakthecloud/kiwi/pkg/fleethost"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrIdempotentConflict = errors.New("idempotent conflict")

// Service turns a high-level task into an immutable manifest and enqueues its
// worker specs onto the lease queue for daemons to pick up. Credentials are NOT
// attached here — they are sealed to the specific daemon's public key at
// delivery time (heartbeat), not at plan time.
type Service struct {
	store   store.Store
	planner Planner
	embed   provider.Embedder
	// fleetHost wakes the machine the free-tier provisioner runs on. Nil is
	// valid and means "no host to manage" (BYOC, local dev).
	fleetHost fleethost.Controller
	// indexSync runs learning indexing inline instead of in a background
	// goroutine. Production leaves it false (best-effort, non-blocking); tests
	// set it true so the write is observable without racing a goroutine.
	indexSync bool
	// newCompleter overrides how a planning Completer is built for a model.
	// Production leaves it nil and gets a real provider; tests set it so the
	// live code path — key resolution, provider routing, cost aggregation —
	// runs without a network call. Injecting a whole Planner instead would skip
	// that path entirely and prove nothing about it.
	newCompleter func(model string) Completer
}

func NewService(s store.Store, p Planner, e provider.Embedder) *Service {
	return &Service{store: s, planner: p, embed: e}
}

// WithFleetHost attaches the fleet-host controller woken on free-tier submit.
func (s *Service) WithFleetHost(c fleethost.Controller) *Service {
	s.fleetHost = c
	return s
}

// SubmitResult reports what the planner generated.
type SubmitResult struct {
	ManifestID string   `json:"manifest_id"`
	JobID      string   `json:"job_id"`
	TaskIDs    []string `json:"task_ids"`
	Summary    string   `json:"summary"`
}

// SubmitPlan runs the planner, persists an immutable content-addressed
// manifest, and enqueues one QueuedTask per planned worker.
func (s *Service) SubmitPlan(ctx context.Context, req PlanRequest) (*SubmitResult, error) {
	if req.OrgID == "" {
		return nil, fmt.Errorf("org id is required")
	}

	if req.IdempotencyKey != "" {
		var sub store.PlanSubmission
		if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND idempotency_key = ?", req.OrgID, req.IdempotencyKey).First(&sub).Error; err == nil {
			var tasks []store.QueuedTask
			if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND job_id = ?", req.OrgID, sub.JobID).Order("id asc").Find(&tasks).Error; err != nil {
				return nil, err
			}
			taskIDs := make([]string, len(tasks))
			for i, t := range tasks {
				taskIDs[i] = t.ID
			}
			return &SubmitResult{
				ManifestID: "deduplicated",
				JobID:      sub.JobID,
				TaskIDs:    taskIDs,
				Summary:    "Deduplicated run",
			}, nil
		}
	}

	// Resolve prior-work learnings before planning. Everything is org-scoped in
	// the store queries — a caller can never reference another tenant's jobs.
	// taskVec is captured so an "auto" submit reuses its query embedding for the
	// post-plan indexing write instead of paying for a second embed call.
	var resolved []store.JobLearning
	var taskVec []float32
	switch req.ReferenceMode {
	case "manual":
		if len(req.ReferenceJobIDs) > 0 {
			resolved, _ = s.store.GetJobLearnings(ctx, req.OrgID, req.ReferenceJobIDs)
			if len(resolved) > 3 {
				resolved = resolved[:3]
			}
		}
	case "auto":
		if s.embed != nil {
			if vec, err := s.embed.Embed(ctx, req.Task); err == nil {
				taskVec = vec
				resolved, _ = s.store.SearchJobLearnings(ctx, req.OrgID, vec, 3, "")
			}
		}
	}
	req.ResolvedLearnings = resolved

	var p Planner
	var completers []Completer
	actualModel := "heuristic"
	prov := "local"
	// True when planning ran on a Control-Plane key rather than the org's, in
	// which case the org did not pay for it and must not be billed for it.
	plannedOnOperatorKey := false
	funding := store.FundingBYOK

	sessionMode := req.Mode == agent.ModeSession

	// An operator kill-switch. Session mode is opt-in per task, but a mode that
	// runs long agentic conversations on customer keys needs a way to be turned
	// off across a fleet without a deploy rollback.
	if sessionMode && os.Getenv("KIWI_SESSION_MODE") == "off" {
		return nil, fmt.Errorf("session mode is disabled on this deployment")
	}

	switch {
	case sessionMode:
		// Nothing is decomposed and no model is called here: the Architect plans
		// inside the daemon, on the customer's key, in their own cloud. The
		// Control Plane still CHOOSES the models, so they are recorded accurately
		// even though it no longer calls them.
		actualModel = req.ArchitectModel
		if actualModel == "" {
			actualModel = req.Model
		}
		prov = provider.ProviderOf(actualModel)
		p = NewSessionPlanner()

		// Submitting used to fail here for an org with no provider key, because
		// the Control Plane had to read that key in order to plan. It no longer
		// reads one — which would turn a clear, immediate error into a task that
		// sits in the queue and fails minutes later inside the daemon. A presence
		// check keeps the old answer without the old access: it asks whether the
		// row exists, and never decrypts it.
		if err := s.requireProviderKey(ctx, req.OrgID, prov); err != nil {
			return nil, err
		}
		if err := s.requireAllowance(ctx, req.OrgID, actualModel); err != nil {
			return nil, err
		}
	case s.planner != nil:
		p = s.planner
	case os.Getenv("KIWI_PLANNER") != "llm":
		p = NewHeuristicPlanner()
	default:
		actualModel = req.PlannerModel
		if actualModel == "" {
			actualModel = os.Getenv("KIWI_PLANNER_MODEL")
			if actualModel == "" {
				actualModel = "claude-opus-4-8"
			}
		}

		prov = provider.ProviderOf(actualModel)

		var key string
		if override := os.Getenv("KIWI_PLANNER_API_KEY"); override != "" {
			key = override
			funding = store.FundingKiwi // Operator keys are Kiwi-funded
			plannedOnOperatorKey = true
		} else {
			if err := s.requireAllowance(ctx, req.OrgID, actualModel); err != nil {
				return nil, err
			}
			var err error
			key, funding, err = s.resolveKey(ctx, req.OrgID, actualModel)
			if err != nil {
				return nil, err
			}
		}

		// Planning runs on the Control Plane with the org's decrypted key. In BYOC
		// that means Kiwi's network makes provider calls with a customer credential
		// — acceptable for managed, a containment gap for BYOC. Moving planning into
		// the daemon (as the Actor/Critic already are) closes it and is the intended
		// direction; it needs a two-phase handoff (queue a plan task, daemon plans,
		// reports the DAG, CP expands it into workers) and is deliberately not done
		// here.
		p = NewLLMPlannerFunc(func(m string) Completer {
			var comp Completer
			switch {
			case s.newCompleter != nil:
				comp = s.newCompleter(m)
			case prov == provider.ProviderGemini:
				comp = provider.NewGeminiProviderWithModels(key, m, m)
			case prov == provider.ProviderOpenAI:
				comp = provider.NewOpenAIProviderWithModels(key, m, m)
			default:
				comp = provider.NewAnthropicProviderWithModels(key, m, m)
			}
			completers = append(completers, comp)
			return comp
		}, actualModel)
	}

	plan, err := p.Plan(ctx, req)
	if err != nil {
		return nil, err
	}

	// Gate the WORKER models, here, after planning, because this is the only
	// point every branch of the switch above reaches.
	//
	// The per-branch checks gate `actualModel`, which is the *planner's* model.
	// That is the wrong model and, worse, two of the three branches never ran a
	// check at all: the heuristic branch is the live path (nothing in the
	// deployment sets KIWI_PLANNER=llm), so a submit whose workers run on a
	// Kiwi-funded model was admitted with no entitlement check anywhere.
	seen := map[string]bool{}
	for _, w := range plan.Workers {
		if w.Model == "" || seen[w.Model] {
			continue
		}
		seen[w.Model] = true
		if err := s.requireEntitlement(ctx, req.OrgID, req.FleetID, w.Model); err != nil {
			return nil, err
		}
	}

	// Planner spend, attributed to the org that paid for it. When the operator
	// override supplied the key the org did not pay, so nothing is recorded
	// against the job — billing a customer for a call made on Kiwi's credential
	// would overstate their spend on a page they cannot reconcile against their
	// own provider invoice.
	var totalCost float64
	var totalIn, totalOut int64
	if !plannedOnOperatorKey {
		for _, c := range completers {
			if ur, ok := c.(UsageReporter); ok {
				in, out, cost := ur.Usage()
				totalIn += in
				totalOut += out
				totalCost += cost
			}
		}
	}

	// A missing limits row, or one written before MaxWorkersPerJob existed,
	// leaves the field at zero. Treating that as a real cap would reject every
	// plan (1 > 0), so the default applies to any non-positive value — matching
	// how LeaseNextTask defaults the same limit.
	var limits store.OrgLimits
	if err := s.store.DB().WithContext(ctx).Where("org_id = ?", req.OrgID).First(&limits).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	maxWorkers := limits.MaxWorkersPerJob
	if maxWorkers <= 0 {
		maxWorkers = defaultMaxWorkersPerJob
	}

	if err := plan.Validate(maxWorkers); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}

	workers := make([]map[string]interface{}, 0, len(plan.Workers))
	for _, w := range plan.Workers {
		workers = append(workers, map[string]interface{}{
			"id":              w.ID,
			"task":            w.Task,
			"file":            w.File,
			"files":           w.Files,
			"model":           w.Model,
			"test_cmd":        workerTestCmd(w, req),
			"depends_on":      w.DependsOn,
			"mode":            w.Mode,
			"architect_model": w.ArchitectModel,
		})
	}
	content := map[string]interface{}{
		"task":     req.Task,
		"repo_url": req.RepoURL,
		"ref":      req.Ref,
		"summary":  plan.Summary,
		"workers":  workers,
		// In session mode this is the model the Architect will run on. The
		// Control Plane still selects it, so the record is accurate at submit
		// even though the call happens later and elsewhere; only the COST arrives
		// afterwards, reported by the daemon.
		"planner_model":    actualModel,
		"planner_provider": prov,
		"mode":             planMode(req),
	}

	manifestID, err := contentHash(content)
	if err != nil {
		return nil, err
	}
	m := &store.Manifest{
		ID:            manifestID,
		OrgID:         req.OrgID,
		SchemaVersion: "1.0",
		Content:       content,
		Producer:      "planner",
		CreatedAt:     time.Now(),
	}
	// Persist the manifest and enqueue all worker tasks atomically: if any
	// enqueue fails, the manifest is rolled back too — no partial plans.
	jobID := "job_" + randHex(8)
	taskIDs := make([]string, 0, len(plan.Workers))
	err = s.store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if req.IdempotencyKey != "" {
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&store.PlanSubmission{
				OrgID:          req.OrgID,
				IdempotencyKey: req.IdempotencyKey,
				JobID:          jobID,
			})
			if res.Error != nil {
				return fmt.Errorf("persist idempotency key: %w", res.Error)
			}
			if res.RowsAffected == 0 {
				return ErrIdempotentConflict
			}
		}

		var ik *string
		if req.IdempotencyKey != "" {
			ik = &req.IdempotencyKey
		}
		job := &store.Job{
			ID:             jobID,
			OrgID:          req.OrgID,
			UserID:         req.UserID,
			Status:         "PENDING",
			IdempotencyKey: ik,
			Inputs: map[string]interface{}{
				"task":     req.Task,
				"repo_url": req.RepoURL,
				"ref":      req.Ref,
				"file":     req.File,
				"test_cmd": req.TestCmd,
				// Recorded so planner spend can be attributed to the model that
				// incurred it. Without it, a cost-by-model breakdown silently
				// omits planning — which is the half most worth acting on, since
				// the planner defaults to the most expensive model available.
				"planner_model": actualModel,
				// The resolved provider, so the spend page can attribute planner
				// spend without re-deriving it from the model id.
				"planner_provider": prov,
			},
			Funding:          funding,
			PlannerCostUSD:   totalCost,
			PlannerTokensIn:  totalIn,
			PlannerTokensOut: totalOut,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(job).Error; err != nil {
			return fmt.Errorf("persist job: %w", err)
		}

		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(m).Error; err != nil {
			return fmt.Errorf("persist manifest: %w", err)
		}
		for _, w := range plan.Workers {
			taskID := jobID + "-" + w.ID
			spec := map[string]interface{}{
				"id":         taskID,
				"task":       w.Task,
				"job_task":   req.Task,
				"file":       w.File,
				"model":      w.Model,
				"test_cmd":   workerTestCmd(w, req),
				"depends_on": w.DependsOn,
				"repo_url":   req.RepoURL,
				"ref":        req.Ref,
				"job_id":     jobID,
			}
			// Resolve the worker's OWN model. The job-level `funding` above
			// describes the planner call; a task inherits nothing from it,
			// because the planner and the worker routinely run on different
			// models with different providers and different payers.
			//
			// The resolved provider and tier are pinned onto the spec rather
			// than re-derived later: the daemon has no catalog to resolve
			// against, and metering must charge the tier the submit was
			// admitted against even if a refresh reprices the model mid-run.
			taskFunding := store.FundingBYOK
			if res, rerr := s.store.ResolveModel(ctx, req.OrgID, w.Model); rerr == nil {
				spec["provider"] = res.Provider
				if res.KiwiProvided {
					if _, ok := provider.PlatformKeyFor(res.Provider); ok {
						spec["tier"] = res.Tier
						taskFunding = store.FundingKiwi
					}
				}
			}
			if w.Mode != "" {
				spec["mode"] = w.Mode
			}
			if w.ArchitectModel != "" {
				spec["architect_model"] = w.ArchitectModel
			}
			// Learnings are resolved here, on the Control Plane, because it owns
			// the vector index — and consumed in the daemon, because in session
			// mode that is where planning happens. Without this they would be
			// searched for, paid for, and thrown away.
			if sessionMode && len(resolved) > 0 {
				spec["learnings"] = learningSummaries(resolved)
			}
			if err := tx.Create(&store.QueuedTask{
				ID:      taskID,
				OrgID:   req.OrgID,
				JobID:   jobID,
				FleetID: req.FleetID,
				Status:  store.TaskQueued,
				Funding: taskFunding,
				Spec:    spec,
			}).Error; err != nil {
				return fmt.Errorf("enqueue task %s: %w", taskID, err)
			}
			taskIDs = append(taskIDs, taskID)
		}
		return nil
	})
	if err == ErrIdempotentConflict {
		var sub store.PlanSubmission
		if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND idempotency_key = ?", req.OrgID, req.IdempotencyKey).First(&sub).Error; err == nil {
			var tasks []store.QueuedTask
			if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND job_id = ?", req.OrgID, sub.JobID).Order("id asc").Find(&tasks).Error; err != nil {
				return nil, err
			}
			taskIDs := make([]string, len(tasks))
			for i, t := range tasks {
				taskIDs[i] = t.ID
			}
			return &SubmitResult{
				ManifestID: "deduplicated",
				JobID:      sub.JobID,
				TaskIDs:    taskIDs,
				Summary:    "Deduplicated run",
			}, nil
		}
	}
	if err != nil {
		return nil, err
	}

	// Best-effort, non-fatal indexing of this job as a learning for future
	// reference. It must never fail an already-accepted submission, so errors are
	// swallowed and (in production) it runs off the request goroutine.
	index := func(taskVec []float32) {
		// A detached, bounded context: the request's context may already be
		// canceled by the time this runs.
		ictx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Reuse the auto-mode query embedding when we have it; only embed here
		// when we don't (manual/off submits), so auto never embeds twice.
		if taskVec == nil && s.embed != nil {
			if v, err := s.embed.Embed(ictx, req.Task); err == nil {
				taskVec = v
			}
		}
		var vec *pgvector.Vector
		if taskVec != nil {
			pv := pgvector.NewVector(taskVec)
			vec = &pv
		}

		// One learning per job: key the row on the job id so a re-index updates in
		// place rather than duplicating (UpsertJobLearning conflicts on job_id).
		learning := &store.JobLearning{
			ID:        jobID,
			JobID:     jobID,
			OrgID:     req.OrgID,
			Repo:      store.ShortRepo(req.RepoURL),
			Task:      req.Task,
			Summary:   plan.Summary,
			Embedding: vec,
		}
		_ = s.store.UpsertJobLearning(ictx, learning)
	}
	if s.indexSync {
		index(taskVec)
	} else {
		go index(taskVec)
	}

	return &SubmitResult{
		ManifestID: manifestID,
		JobID:      jobID,
		TaskIDs:    taskIDs,
		Summary:    plan.Summary,
	}, nil
}

// workerTestCmd resolves the test command for a worker: the submitter's command
// wins, then anything a planner supplied.
//
// The order matters, and it is the reverse of what it was. The test command is
// the definition of done, and the planner picks one without ever seeing the
// repo — so it guesses, and a guess like "npm test" against a project with no
// test script does not fail cleanly, it *errors*, which is indistinguishable
// from a failing test. The loop then spends its whole budget trying to make a
// script that does not exist pass.
//
// Empty is the useful answer when nobody supplied one: the daemon infers the
// command from the repository's own marker files (inferTestCmd), which is a
// decision made with the repo in hand rather than from its URL. A planner value
// here would suppress that, so the LLM planner no longer emits one at all.
func workerTestCmd(w PlannedWorker, req PlanRequest) string {
	if req.TestCmd != "" {
		return req.TestCmd
	}
	return w.TestCmd
}

// contentHash returns the SHA-256 of the canonical JSON encoding of content
// (Go sorts map keys, giving a stable, content-addressed manifest id).
func contentHash(content map[string]interface{}) (string, error) {
	b, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// planMode reports the execution mode a request will run in, normalised so the
// manifest never records an empty string for what is really the default.
func planMode(req PlanRequest) string {
	if req.Mode == agent.ModeSession {
		return agent.ModeSession
	}
	return agent.ModeFileLoop
}

// learningSummaries reduces resolved prior jobs to the lines the Architect can
// actually use. It carries the task and its summary and nothing else: a
// learning is context, not evidence, and passing identifiers or URLs invites a
// model to treat it as something to go and look up.
func learningSummaries(learnings []store.JobLearning) []string {
	out := make([]string, 0, len(learnings))
	for _, l := range learnings {
		switch {
		case l.Summary != "" && l.Task != "":
			out = append(out, fmt.Sprintf("%s — %s", l.Task, l.Summary))
		case l.Summary != "":
			out = append(out, l.Summary)
		case l.Task != "":
			out = append(out, l.Task)
		}
	}
	return out
}

// requireProviderKey fails a submit when the org has no key for the provider
// its models need.
//
// Presence only: it reads the credential's metadata row and never decrypts it,
// so session mode keeps the Control Plane out of customer plaintext while still
// failing fast. The message is phrased exactly as the planning path's was, so
// the dashboard's error mapper still recognises it and offers Integrations.
func (s *Service) requireProviderKey(ctx context.Context, orgID, prov string) error {
	want := provider.CredentialNameFor(prov)
	creds, err := s.store.ListCredentials(ctx, orgID)
	if err != nil {
		// A lookup failure is not evidence of a missing key. Letting the submit
		// through means the daemon reports the real problem a minute later, which
		// is better than refusing work over a transient database error.
		return nil
	}
	for _, c := range creds {
		if c.Name == want {
			return nil
		}
	}
	return fmt.Errorf("no %s provider key connected for planning: add one in Integrations", prov)
}

// resolveKey returns the API key to plan with and which budget it draws from.
//
// A Kiwi-funded model uses Kiwi's own key and reports funding "kiwi"; anything
// else uses the org's stored credential and reports "byok". The funding value
// is recorded on the job so the spend page can keep Kiwi-covered work out of
// the dollar total the org owes.
func (s *Service) resolveKey(ctx context.Context, orgID, model string) (string, string, error) {
	res, err := s.store.ResolveModel(ctx, orgID, model)
	if err == nil && res.KiwiProvided {
		if key, ok := provider.PlatformKeyFor(res.Provider); ok {
			return key, store.FundingKiwi, nil
		}
	}

	prov := provider.ProviderOf(model)
	if err == nil && res.Provider != "" {
		prov = res.Provider
	}
	secretName := provider.CredentialNameFor(prov)
	key, kerr := s.store.GetCredentialPlaintext(ctx, orgID, secretName)
	if kerr != nil || key == "" {
		// Phrased so the dashboard's error mapper recognises it as a credential
		// problem and offers the link to Integrations.
		return "", "", fmt.Errorf("no %s provider key connected for planning: add one in Integrations", prov)
	}
	return key, store.FundingBYOK, nil
}

// requireEntitlement fails a submit that asks Kiwi to pay for work it cannot.
//
// It runs beside requireProviderKey for the same reason that one exists: an
// immediate, actionable error at submit beats a task that sits in the queue and
// fails minutes later inside the daemon with a confusing reason.
//
// A BYOK model returns nil immediately — the org's own allowance is their own
// business.
func (s *Service) requireEntitlement(ctx context.Context, orgID, fleetID, model string) error {
	res, err := s.store.ResolveModel(ctx, orgID, model)
	if err != nil {
		// A lookup failure is not evidence of an entitlement problem. Let the
		// submit through; the daemon reports the real problem if there is one.
		return nil
	}
	if !res.KiwiProvided {
		return nil
	}
	// Nothing to fund if Kiwi holds no key for the provider, whatever the
	// catalog row says. Without this, an org is refused a model it is perfectly
	// able to run on its own key.
	if _, ok := provider.PlatformKeyFor(res.Provider); !ok {
		return nil
	}

	// Kiwi keys only ever reach daemons Kiwi operates, so a Kiwi-funded model on
	// any other fleet would be admitted and then fail with no key at all.
	//
	// This asks about the fleet the task is actually queued to, not whether the
	// org happens to own some Kiwi-operated fleet somewhere: an org with both
	// kinds submitting to its BYOC fleet would otherwise be admitted here and
	// fail at heartbeat — the exact outcome this function exists to prevent.
	if !s.fleetCanUseKiwiKeys(ctx, orgID, fleetID) {
		return fmt.Errorf("%s is a Kiwi-provided model and runs only on a Kiwi-managed fleet; select a Kiwi-managed fleet or use your own provider key", model)
	}

	return s.requireAllowance(ctx, orgID, model)
}

// requireAllowance is the fleet-independent half of requireEntitlement.
//
// The planner runs on the Control Plane and calls the provider directly, so no
// fleet is involved and no key is sealed anywhere — but the allowance still
// applies, because the call is still made on Kiwi's credential.
func (s *Service) requireAllowance(ctx context.Context, orgID, model string) error {
	res, err := s.store.ResolveModel(ctx, orgID, model)
	if err != nil || !res.KiwiProvided {
		return nil
	}
	if _, ok := provider.PlatformKeyFor(res.Provider); !ok {
		return nil
	}

	plan, err := s.store.GetOrgPlan(ctx, orgID)
	if err != nil {
		// A lookup failure is not evidence of an entitlement problem. Let the
		// submit through rather than blocking work on a transient DB error.
		return nil
	}
	checker := &entitlement.Checker{Store: s.store}
	allowed, err := checker.Allow(ctx, orgID, plan, res.Tier)
	if err != nil {
		if errors.Is(err, entitlement.ErrNotGrantable) {
			return fmt.Errorf("%s cannot be run on a Kiwi-provided key; connect your own provider key in Integrations", model)
		}
		return nil
	}
	if !allowed {
		return fmt.Errorf("your monthly %s token allowance is exhausted; connect your own provider key in Integrations or wait for the allowance to reset", res.Tier)
	}
	return nil
}

// fleetCanUseKiwiKeys reports whether a task queued to this fleet could be
// handed a Kiwi-owned key at lease time.
//
// It must agree exactly with store.IsKiwiOperatedFleet, which is the gate that
// actually decides. Answering "yes" here for a fleet that gate will refuse
// produces a task that queues and then dies for want of a key; answering "no"
// for one it would accept refuses work that was perfectly runnable. Deferring
// to the same function is the only way the two cannot drift.
func (s *Service) fleetCanUseKiwiKeys(ctx context.Context, orgID, fleetID string) bool {
	ok, err := s.store.IsKiwiOperatedFleet(ctx, orgID, fleetID)
	if err != nil {
		// Do not refuse a submit over a lookup failure. The lease-time gate is
		// the one that protects the credential; this one only protects the user
		// from a confusing delayed failure.
		return true
	}
	return ok
}
