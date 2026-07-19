package orchestrator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agentapi"
	"github.com/ibreakthecloud/kiwi/pkg/audit"
	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/billing"
	"github.com/ibreakthecloud/kiwi/pkg/checkpoint"
	"github.com/ibreakthecloud/kiwi/pkg/dashboard"
	"github.com/ibreakthecloud/kiwi/pkg/infra"
	"github.com/ibreakthecloud/kiwi/pkg/planner"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TaskState represents the status and progress of a cloud task
type TaskState struct {
	ID             string    `json:"id" gorm:"primaryKey"`
	Task           string    `json:"task"`
	FilePath       string    `json:"file_path"`
	TestCmd        string    `json:"test_cmd"`
	Status         string    `json:"status"` // "RUNNING", "SUCCESS", "FAILED", "PAUSED"
	Logs           string    `json:"logs"`
	CreatedAt      time.Time `json:"created_at"`
	SandboxPath    string    `json:"-"`
	Cost           float64   `json:"cost"`
	IdempotencyKey string    `json:"-" gorm:"index"`
	UserID         string    `json:"user_id" gorm:"index"`
	OrgID          string    `json:"org_id" gorm:"index"`
	UserEmail      string    `json:"user_email"`
}

type Server struct {
	db           *gorm.DB
	storage      store.Store
	launchFn     func(taskID, sandboxPath string, manifest *store.Manifest)
	infra        infra.Infra
	snapshotRoot string // where checkpoint blobs live (durable, outside the ephemeral sandbox)
	agentAPI     *agentapi.Server
	planner      *planner.Service
	// credValidator confirms a provider credential is accepted before it is
	// saved. nil skips validation (kept nil in unit tests that construct Server
	// directly so they make no external calls); NewServer wires the real one.
	credValidator func(ctx context.Context, name, value string) error
	httpServer    *http.Server
}

// selectPlanner chooses the planner from the environment. With
// KIWI_PLANNER=llm and a Control-Plane planning key (KIWI_PLANNER_API_KEY, or
// ANTHROPIC_API_KEY as a fallback), it wires the frontier-model LLMPlanner via
// the provider's Completer; otherwise it uses the deterministic HeuristicPlanner
// (the offline/default path). The planner runs on the CP's own key, not a
// customer's (Execution Model RFC §4.1).
func selectPlanner() planner.Planner {
	if os.Getenv("KIWI_PLANNER") != "llm" {
		return planner.NewHeuristicPlanner()
	}
	key := os.Getenv("KIWI_PLANNER_API_KEY")
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		fmt.Println("[planner] KIWI_PLANNER=llm but no planning key set; falling back to HeuristicPlanner")
		return planner.NewHeuristicPlanner()
	}
	model := os.Getenv("KIWI_PLANNER_MODEL")
	if model == "" {
		model = "claude-opus-4-8"
	}
	fmt.Printf("[planner] Using LLMPlanner (model %s)\n", model)
	return planner.NewLLMPlanner(provider.NewAnthropicProviderWithModels(key, model, model))
}

func NewServer(storage store.Store, role string) *Server {
	root := os.Getenv("KIWI_SNAPSHOT_DIR")
	if root == "" {
		root = filepath.Join(os.TempDir(), "kiwi-snapshots")
	}
	s := &Server{
		db:           storage.DB(),
		storage:      storage,
		infra:        infra.NewDockerInfra(os.TempDir()),
		snapshotRoot: root,
		// Planner defaults to the deterministic HeuristicPlanner; the
		// frontier-model LLMPlanner is selected via env (see selectPlanner).
		planner:       planner.NewService(storage, selectPlanner()),
		credValidator: defaultCredValidator,
	}
	// Sandbox-facing Agent API: scoped-token authorized, secrets bridged to the
	// reverse tunnel, events into the durable log (issue #34).
	s.agentAPI = agentapi.NewServer(agentapi.Deps{
		Store:   storage,
		Events:  checkpoint.NewService(storage, checkpoint.NewLocalSnapshotter(root)),
		Secrets: tunnelSecrets{},
	})
	if role == "all" || role == "orchestrator" {
		s.launchFn = s.LaunchTask
	} else {
		s.launchFn = nil // No orchestrator consumer in 'api' mode yet
	}
	return s
}

// tunnelSecrets bridges the Agent API's FetchSecret to the reverse credential
// tunnel keyed by job/task id.
type tunnelSecrets struct{}

func (tunnelSecrets) Resolve(ctx context.Context, jobID, key string) (string, error) {
	t := tunnel.GlobalRegistry.Get(jobID)
	if t == nil {
		return "", fmt.Errorf("no credential tunnel registered for job %s", jobID)
	}
	return t.GetSecret(ctx, key)
}

// LaunchTask runs the orchestration loop in the background for an
// already-persisted task whose sandbox is populated on disk. Used by both
// submission and boot recovery. It seeds the log buffer from the existing row so
// recovered tasks keep their prior logs.
func (s *Server) LaunchTask(taskID, sandboxPath string, manifest *store.Manifest) {
	go func() {
		logBuf := new(bytes.Buffer)
		var existing TaskState
		if err := s.db.First(&existing, "id = ?", taskID).Error; err == nil {
			logBuf.WriteString(existing.Logs)
		}

		_ = audit.LogEventDirect(s.db, existing.OrgID, existing.UserID, existing.UserEmail, "EXECUTE", "TASK", taskID, fmt.Sprintf("Started background loop execution in %s", sandboxPath), "")

		limits, err := auth.GetOrgLimits(s.db, existing.OrgID)
		if err != nil {
			fmt.Fprintf(logBuf, "[Orchestrator] Error resolving organization limits: %v. Using default limit configurations.\n", err)
			limits = auth.DefaultLimits(existing.OrgID)
		}

		var apiKey string
		var actorModel = "claude-opus-4-8"
		var criticModel = "claude-opus-4-8"

		provConfig, err := auth.GetProviderConfig(s.db, existing.OrgID)
		if err == nil && provConfig != nil {
			if provConfig.ActorModel != "" {
				actorModel = provConfig.ActorModel
			}
			if provConfig.CriticModel != "" {
				criticModel = provConfig.CriticModel
			}
			if decKey, err := provConfig.DecryptKey(); err == nil && decKey != "" {
				apiKey = decKey
			}
		}

		var engine *Engine
		if os.Getenv("KIWI_LLM_PROVIDER") == "anthropic" {
			if apiKey != "" {
				ap := provider.NewAnthropicProviderWithModels(apiKey, actorModel, criticModel)
				engine = NewEngine(ap, 5)
				engine.Critic = ap
				engine.LLMMode = "anthropic"
			} else {
				engine = NewEngine(nil, 5) // provider built lazily after key resolution
				engine.LLMMode = "anthropic"
			}
			engine.ActorModel = actorModel
			engine.CriticModel = criticModel
		} else {
			engine = NewEngine(provider.NewMockProvider(), 5)
			engine.Critic = provider.NewMockCritic()
			engine.LLMMode = "mock"
		}
		engine.MaxBudget = limits.MaxBudgetPerTask
		engine.LogOut = logBuf
		engine.StateCallback = func(newStatus string) {
			s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("status", newStatus)
		}
		engine.CostCallback = func(amount float64) {
			s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("cost", gorm.Expr("cost + ?", amount))
		}
		orgID := existing.OrgID
		engine.EventCallback = func(ev TaskEvent) {
			ev.TaskID = taskID
			ev.OrgID = orgID
			_ = s.db.Create(&ev).Error // best-effort telemetry; never fail the task
		}

		// Infra dependency injection
		// Initialized once in NewServer
		engine.Infra = s.infra

		// Durability: event log + workspace checkpoints + side-effect ledger.
		// taskID is used as the V2 jobID. Snapshots live under snapshotRoot so
		// they survive sandbox cleanup and daemon restarts; on relaunch the
		// engine restores the latest checkpoint and replays the loop tail.
		snap := checkpoint.NewLocalSnapshotter(s.snapshotRoot)
		engine.Checkpoints = checkpoint.NewService(s.storage, snap)
		engine.Ledger = checkpoint.NewLedger(s.storage)

		// Ensure a V2 Job row exists (idempotent across submission + recovery).
		jobStatus := "RUNNING"
		sref := sandboxPath
		inputs := map[string]interface{}{}
		if manifest != nil {
			inputs = manifest.Content
		}
		job := &store.Job{
			ID:         taskID,
			OrgID:      existing.OrgID,
			UserID:     existing.UserID,
			Status:     jobStatus,
			Inputs:     inputs,
			SandboxRef: &sref,
		}
		if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(job).Error; err != nil {
			fmt.Fprintf(logBuf, "[Orchestrator] Warning: could not persist V2 job row: %v\n", err)
		}

		// Mint a short-lived scoped token so the in-sandbox agent can reach the
		// control plane through the Agent API for this job only (issue #34). The
		// plaintext is available here to inject into the sandbox env; only its
		// hash is persisted. TTL tracks the task timeout with headroom.
		jobTokenTTL := time.Duration(limits.TaskTimeoutMinutes)*time.Minute + 5*time.Minute
		jobToken, err := agentapi.MintJobToken(s.db, taskID, existing.OrgID, jobTokenTTL)
		if err != nil {
			fmt.Fprintf(logBuf, "[Orchestrator] Warning: could not mint job token: %v\n", err)
		} else {
			// TODO(#35): plumb jobToken to the sandbox (inject as KIWI_JOB_TOKEN via
			// Infra) so the in-sandbox kiwi-agent can call the Agent API. For now
			// only the hash is persisted; the plaintext is not yet delivered to the
			// sandbox, so it is intentionally not used here.
			_ = jobToken
			fmt.Fprintln(logBuf, "[Orchestrator] Minted scoped Agent-API token for this job.")
		}

		taskTimeoutVal := time.Duration(limits.TaskTimeoutMinutes) * time.Minute
		ctx, cancel := context.WithTimeout(context.Background(), taskTimeoutVal)
		defer cancel()

		ctx = context.WithValue(ctx, sandbox.SandboxConfigKey, &sandbox.SandboxConfig{
			UseDocker:   os.Getenv("USE_DOCKER") == "true",
			DockerImage: limits.DockerImage,
			MemoryLimit: fmt.Sprintf("%dm", limits.MaxSandboxMemoryMB),
			CPULimit:    fmt.Sprintf("%.1f", limits.MaxSandboxCPU),
			NetworkNone: true,
		})

		// Periodic background log synchronizer to SQLite row (every 500ms)
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("logs", logBuf.String())
					return
				case <-ticker.C:
					s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("logs", logBuf.String())
				}
			}
		}()

		fmt.Fprintf(logBuf, "[Orchestrator] Running task in sandbox: %s\n", sandboxPath)

		err = engine.RunTask(ctx, taskID, sandboxPath, manifest)

		finalStatus := "SUCCESS"
		if err != nil {
			finalStatus = "FAILED"
			fmt.Fprintf(logBuf, "\n[Execution Failure]: %v\n", err)
		} else {
			fmt.Fprintln(logBuf, "\n[Execution Success]: Task completed successfully.")
			fixedZip, zerr := sandbox.ZipDir(sandboxPath)
			if zerr == nil {
				_ = os.WriteFile(filepath.Join(sandboxPath, "output.zip"), fixedZip, 0644)
			}
		}

		s.db.Model(&TaskState{}).Where("id = ?", taskID).Updates(map[string]interface{}{
			"status": finalStatus,
			"logs":   logBuf.String(),
		})

		// Mirror the terminal state into the V2 job (SUCCESS->SUCCEEDED).
		v2Status := "SUCCEEDED"
		if finalStatus == "FAILED" {
			v2Status = "FAILED"
		}
		s.db.Model(&store.Job{}).Where("id = ?", taskID).Update("status", v2Status)

		_ = audit.LogEventDirect(s.db, existing.OrgID, existing.UserID, existing.UserEmail, "EXECUTE", "TASK", taskID, fmt.Sprintf("Finished execution with status: %s", finalStatus), "")

		tunnel.GlobalRegistry.Deregister(taskID)
	}()
}

// GenerateTaskID generates a short unique hex ID
// taskTimeout returns the per-task context timeout from KIWI_TASK_TIMEOUT
// (Go duration string), defaulting to 10 minutes.
func taskTimeout() time.Duration {
	if v := os.Getenv("KIWI_TASK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 10 * time.Minute
}

// maxBudget returns the per-task USD budget from KIWI_MAX_BUDGET, defaulting to $1.00.
func maxBudget() float64 {
	if v := os.Getenv("KIWI_MAX_BUDGET"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 1.00
}

func generateTaskID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// authenticate extracts and validates user identity from the request,
// returning claims on success or writing an HTTP error on failure.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) *auth.UserClaims {
	claims, err := auth.AuthFunc(s.db, r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return nil
	}
	return claims
}

// authorizeTask verifies the caller owns the task (same org) or is a system admin.
func (s *Server) authorizeTask(claims *auth.UserClaims, task *TaskState) bool {
	if claims.IsAdmin() {
		return true
	}
	return claims.OrgID == task.OrgID
}

func (s *Server) handleCORSAndPreflight(w http.ResponseWriter, r *http.Request) bool {
	allowed := os.Getenv("KIWI_CORS_ALLOWED_ORIGINS")
	origin := r.Header.Get("Origin")

	if allowed == "" || allowed == "*" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		origins := strings.Split(allowed, ",")
		matched := false
		for _, o := range origins {
			if strings.TrimSpace(o) == origin {
				matched = true
				break
			}
		}
		if matched {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
	}

	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()

	// Register admin endpoints (auth middleware applied at the mux level below).
	auth.AdminRouter(s.db, mux)

	mux.HandleFunc("/api/v1/planner/plan", s.planner.HandlePlan)
	mux.HandleFunc("/api/v1/credentials", s.handleSetCredential)
	// Org admin mints a daemon join token for their own org (behind auth).
	mux.HandleFunc("/api/v1/daemon/join-token", s.handleDaemonJoinToken)
	mux.HandleFunc("/api/v1/daemons", s.handleDaemonsList)
	mux.HandleFunc("/api/v1/jobs", s.handleJobsList)
	mux.HandleFunc("/api/v1/jobs/", s.handleJobStatus)
	mux.HandleFunc("/api/v1/fleets", s.handleFleets)
	mux.HandleFunc("/api/v1/models", s.handleModels)
	mux.HandleFunc("/api/v1/models/", s.handleModels)
	mux.HandleFunc("/api/v1/integrations", s.handleIntegrations)
	mux.HandleFunc("/api/v1/github/repos", s.handleGithubRepos)
	mux.HandleFunc("/tasks", s.handleTasks)
	mux.HandleFunc("/tasks/", s.handleTaskStatus)
	mux.HandleFunc("/usage", s.handleUsage)
	mux.HandleFunc("/tunnel/", func(w http.ResponseWriter, r *http.Request) {
		claims := auth.ClaimsFromContext(r.Context())
		if claims == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/response") {
			tunnel.HandleTunnelResponse(w, r)
		} else {
			tunnel.HandleTunnelConn(w, r)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/dashboard" {
			dashboard.HandleDashboard(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// The org-authenticated surface (org API keys) is everything except the
	// sandbox Agent API, which authenticates with a per-job scoped token and is
	// mounted separately so it bypasses AuthMiddleware.
	root := http.NewServeMux()
	if s.agentAPI != nil {
		root.Handle("/agent/", s.agentAPI.Handler())
	}
	root.HandleFunc("/api/v1/webhooks/linear/", s.handleLinearWebhook)
	// The daemon API authenticates by Ed25519 request signature, not an org API
	// key, so it is mounted here alongside the webhook to bypass AuthMiddleware.
	root.HandleFunc("/api/v1/daemon/register", s.handleDaemonRegister)
	root.HandleFunc("/api/v1/daemon/heartbeat", s.handleDaemonHeartbeat)
	root.HandleFunc("/api/v1/daemon/renew", s.handleDaemonRenew)
	root.HandleFunc("/api/v1/daemon/result", s.handleDaemonResult)

	root.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	root.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		sqlDB, err := s.db.DB()
		if err != nil {
			http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := sqlDB.PingContext(r.Context()); err != nil {
			http.Error(w, "Database ping failed", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	root.Handle("/", auth.AuthMiddleware(s.db, mux))

	// CORS + rate limiting apply to the whole surface.
	handler := corsMiddleware(s.rateLimitMiddleware(root))

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	fmt.Printf("[Server] Kiwi daemon listening on %s\n", addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server without interrupting any active connections.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// corsMiddleware applies CORS headers to all responses and handles OPTIONS preflight.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := os.Getenv("KIWI_CORS_ALLOWED_ORIGINS")
		origin := r.Header.Get("Origin")

		if allowed == "" || allowed == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			origins := strings.Split(allowed, ",")
			matched := false
			for _, o := range origins {
				if strings.TrimSpace(o) == origin {
					matched = true
					break
				}
			}
			if matched {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}

		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodGet {
		var taskList []*TaskState
		query := s.db.Order("created_at desc")
		// Admins see all tasks; members see only their org's tasks.
		if !claims.IsAdmin() {
			query = query.Where("org_id = ?", claims.OrgID)
		}
		if err := query.Find(&taskList).Error; err != nil {
			http.Error(w, "Failed to load tasks from database", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(taskList)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form
	err := r.ParseMultipartForm(50 * 1024 * 1024) // 50MB max in memory
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse multipart form: %v", err), http.StatusBadRequest)
		return
	}

	task := r.FormValue("task")
	file := r.FormValue("file")
	testCmd := r.FormValue("test_cmd")

	if task == "" || file == "" || testCmd == "" {
		http.Error(w, "missing required parameters: task, file, test_cmd", http.StatusBadRequest)
		return
	}

	// Look up organization limits
	limits, err := auth.GetOrgLimits(s.db, claims.OrgID)
	if err != nil {
		http.Error(w, "Failed to resolve organization resource limits", http.StatusInternalServerError)
		return
	}

	// 1. Check concurrent tasks limit
	ok, err := limits.CheckConcurrentLimit(s.db)
	if err != nil {
		http.Error(w, "Failed to verify concurrent task limit", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, fmt.Sprintf("Too Many Requests: Organization concurrent task limit (%d) reached", limits.MaxConcurrentTasks), http.StatusTooManyRequests)
		return
	}

	// 2. Check monthly budget limit
	ok, err = limits.CheckMonthlyBudget(s.db)
	if err != nil {
		http.Error(w, "Failed to verify monthly budget limit", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, fmt.Sprintf("Forbidden: Organization monthly budget limit ($%.2f) exceeded", limits.MaxBudgetPerMonth), http.StatusForbidden)
		return
	}

	// Idempotent submission: scoped by org to prevent cross-tenant collisions.
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if existing, ok := findByIdempotencyKey(s.db, idempotencyKey, claims.OrgID); ok {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"task_id": existing.ID,
			"status":  existing.Status,
		})
		return
	}

	// Read codebase archive
	codebaseFile, _, err := r.FormFile("codebase")
	if err != nil {
		http.Error(w, "missing codebase file upload", http.StatusBadRequest)
		return
	}
	defer codebaseFile.Close()

	zipData, err := io.ReadAll(codebaseFile)
	if err != nil {
		http.Error(w, "failed to read uploaded codebase archive", http.StatusInternalServerError)
		return
	}

	// Create temporary sandbox directory prefixed with organization ID
	tempSandbox, err := os.MkdirTemp("", fmt.Sprintf("kiwi-server-sandbox-%s-*", claims.OrgID))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create temporary sandbox: %v", err), http.StatusInternalServerError)
		return
	}

	// Extract uploaded workspace
	err = sandbox.UnzipBytes(tempSandbox, zipData)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to extract codebase: %v", err), http.StatusInternalServerError)
		return
	}

	// 3. Enforce sandbox disk quota limit
	diskSize, err := dirSizeMB(tempSandbox)
	if err != nil {
		http.Error(w, "Failed to verify sandbox disk usage", http.StatusInternalServerError)
		os.RemoveAll(tempSandbox)
		return
	}
	if diskSize > float64(limits.MaxSandboxDiskMB) {
		http.Error(w, fmt.Sprintf("Payload Too Large: Sandbox workspace size (%.2f MB) exceeds limit (%d MB)", diskSize, limits.MaxSandboxDiskMB), http.StatusRequestEntityTooLarge)
		os.RemoveAll(tempSandbox)
		return
	}

	taskID := generateTaskID()
	// Pre-register tunnel in registry so it exists before background run starts
	tunnel.GlobalRegistry.Register(taskID, claims.UserID, claims.OrgID)

	state := &TaskState{
		ID:             taskID,
		Task:           task,
		FilePath:       file,
		TestCmd:        testCmd,
		Status:         "PENDING",
		CreatedAt:      time.Now(),
		SandboxPath:    tempSandbox,
		Cost:           0.05,
		IdempotencyKey: idempotencyKey,
		UserID:         claims.UserID,
		OrgID:          claims.OrgID,
		UserEmail:      claims.Email,
	}

	job := &store.Job{
		ID:             taskID,
		OrgID:          claims.OrgID,
		UserID:         claims.UserID,
		Status:         "PENDING",
		IdempotencyKey: &idempotencyKey,
		Inputs: map[string]interface{}{
			"task":     task,
			"file":     file,
			"test_cmd": testCmd,
		},
		SandboxRef: &tempSandbox,
	}
	if idempotencyKey == "" {
		job.IdempotencyKey = nil
	}

	outbox := &store.Outbox{
		JobID: taskID,
		Topic: "jobs.submitted",
		Payload: map[string]interface{}{
			"job_id": taskID,
		},
	}

	// Reuse store method as per P1.2 review
	if err := s.db.Create(state).Error; err != nil {
		http.Error(w, fmt.Sprintf("failed to create task state: %v", err), http.StatusInternalServerError)
		os.RemoveAll(tempSandbox)
		return
	}
	err = s.storage.CreateJobWithOutbox(r.Context(), job, outbox)

	if err != nil {
		http.Error(w, "failed to register task in database", http.StatusInternalServerError)
		return
	}

	_ = auth.LogAuditEvent(s.db, r, "CREATE", "TASK", taskID, fmt.Sprintf("Submitted task %q for file %q", task, file))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"task_id": taskID,
		"status":  "PENDING",
	})
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Events sub-resource: GET /tasks/{id}/events
	if strings.HasSuffix(r.URL.Path, "/events") {
		s.handleTaskEvents(w, r, claims)
		return
	}

	taskID := filepath.Base(r.URL.Path)

	var state TaskState
	if err := s.db.First(&state, "id = ?", taskID).Error; err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// Verify the caller owns this task (same org) or is admin.
	if !s.authorizeTask(claims, &state) {
		http.Error(w, "Forbidden: you do not have access to this task", http.StatusForbidden)
		return
	}

	// Serve the zip output if download=true
	if r.URL.Query().Get("download") == "true" {
		if state.Status != "SUCCESS" {
			http.Error(w, "Task not completed successfully or sandbox unavailable", http.StatusBadRequest)
			return
		}
		zipPath := filepath.Join(state.SandboxPath, "output.zip")
		fileBytes, err := os.ReadFile(zipPath)
		if err != nil {
			http.Error(w, "Result zip not found", http.StatusGone)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"kiwi-%s.zip\"", taskID))
		_, _ = w.Write(fileBytes)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

// handleTaskEvents serves the structured telemetry timeline for a task,
// authorized identically to the task status endpoint (same org or admin).
func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request, claims *auth.UserClaims) {
	// path is /tasks/{id}/events → id is the segment before "/events"
	trimmed := strings.TrimSuffix(r.URL.Path, "/events")
	taskID := filepath.Base(trimmed)

	var state TaskState
	if err := s.db.First(&state, "id = ?", taskID).Error; err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if !s.authorizeTask(claims, &state) {
		http.Error(w, "Forbidden: you do not have access to this task", http.StatusForbidden)
		return
	}

	var events []TaskEvent
	if err := s.db.Where("task_id = ?", taskID).Order("id asc").Find(&events).Error; err != nil {
		http.Error(w, "Failed to load task events", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

// dirSizeMB calculates the total size of all files in a directory in MB.
func dirSizeMB(dir string) (float64, error) {
	var size int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return float64(size) / (1024 * 1024), nil
}

// handleUsage returns the organization-scoped task execution costs and metrics.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	from, to, err := billing.ParseDateParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	usage, err := billing.GetOrgUsage(s.db, claims.OrgID, from, to)
	if err != nil {
		http.Error(w, "Failed to aggregate usage statistics: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(usage)
}

type limiterEntry struct {
	tokens float64
	last   time.Time
}

type RateLimiter struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  float64
	limits map[string]*limiterEntry
}

func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{
		rate:   rate,
		burst:  float64(burst),
		limits: make(map[string]*limiterEntry),
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	lim, exists := rl.limits[key]
	now := time.Now()
	if !exists {
		rl.limits[key] = &limiterEntry{
			tokens: rl.burst - 1.0,
			last:   now,
		}
		return true
	}

	elapsed := now.Sub(lim.last).Seconds()
	lim.last = now
	newTokens := lim.tokens + elapsed*rl.rate
	if newTokens > rl.burst {
		newTokens = rl.burst
	}

	if newTokens >= 1.0 {
		lim.tokens = newTokens - 1.0
		return true
	}

	return false
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	// 10 requests per second rate limit per client, burst 30 requests.
	rl := NewRateLimiter(10.0, 30)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bypass limit check for OPTIONS preflights
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		key := r.RemoteAddr
		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			key = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if !rl.Allow(key) {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
