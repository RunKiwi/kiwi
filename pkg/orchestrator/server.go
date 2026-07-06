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
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/dashboard"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
	"gorm.io/gorm"
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
}

type Server struct {
	db       *gorm.DB
	launchFn func(taskID, sandboxPath, task, file, testCmd string)
}

func NewServer(db *gorm.DB) *Server {
	s := &Server{db: db}
	s.launchFn = s.launchTask
	return s
}

// launchTask runs the orchestration loop in the background for an
// already-persisted task whose sandbox is populated on disk. Used by both
// submission and boot recovery. It seeds the log buffer from the existing row so
// recovered tasks keep their prior logs.
func (s *Server) launchTask(taskID, sandboxPath, task, file, testCmd string) {
	go func() {
		logBuf := new(bytes.Buffer)
		var existing TaskState
		if err := s.db.First(&existing, "id = ?", taskID).Error; err == nil {
			logBuf.WriteString(existing.Logs)
		}

		var engine *Engine
		if os.Getenv("KIWI_LLM_PROVIDER") == "anthropic" {
			engine = NewEngine(nil, 5) // provider built lazily after key resolution
			engine.LLMMode = "anthropic"
		} else {
			engine = NewEngine(provider.NewMockProvider(), 5)
			engine.Critic = provider.NewMockCritic()
			engine.LLMMode = "mock"
		}
		engine.MaxBudget = maxBudget()
		engine.LogOut = logBuf
		engine.StateCallback = func(newStatus string) {
			s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("status", newStatus)
		}
		engine.CostCallback = func(amount float64) {
			s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("cost", gorm.Expr("cost + ?", amount))
		}

		ctx, cancel := context.WithTimeout(context.Background(), taskTimeout())
		defer cancel()

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

		absFilePath := filepath.Join(sandboxPath, file)
		fmt.Fprintf(logBuf, "[Orchestrator] Running task in sandbox: %s\n", sandboxPath)

		err := engine.RunTask(ctx, taskID, sandboxPath, task, absFilePath, testCmd)

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

func (s *Server) validateAuth(w http.ResponseWriter, r *http.Request) bool {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
		return false
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		http.Error(w, "Unauthorized: invalid Authorization header format", http.StatusUnauthorized)
		return false
	}
	token := parts[1]
	expectedToken := os.Getenv("KIWI_SERVER_TOKEN")
	if expectedToken == "" {
		expectedToken = "kiwi-auth-token-1234" // Default developer fallback token
	}
	if token != expectedToken {
		http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) handleCORSAndPreflight(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", s.handleTasks)
	mux.HandleFunc("/tasks/", s.handleTaskStatus)
	mux.HandleFunc("/tunnel/", func(w http.ResponseWriter, r *http.Request) {
		if !s.validateAuth(w, r) {
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

	fmt.Printf("[Server] Kiwi daemon listening on %s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if s.handleCORSAndPreflight(w, r) {
		return
	}
	if !s.validateAuth(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var taskList []*TaskState
		if err := s.db.Order("created_at desc").Find(&taskList).Error; err != nil {
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

	// Create temporary sandbox directory
	tempSandbox, err := os.MkdirTemp("", "kiwi-server-sandbox-*")
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

	taskID := generateTaskID()
	// Pre-register tunnel in registry so it exists before background run starts
	tunnel.GlobalRegistry.Register(taskID)

	state := &TaskState{
		ID:          taskID,
		Task:        task,
		FilePath:    file,
		TestCmd:     testCmd,
		Status:      "RUNNING",
		CreatedAt:   time.Now(),
		SandboxPath: tempSandbox,
		Cost:        0.05,
	}

	if err := s.db.Create(state).Error; err != nil {
		http.Error(w, "failed to register task in database", http.StatusInternalServerError)
		return
	}

	// Launch loop orchestration in the background (injectable for tests).
	s.launchFn(taskID, tempSandbox, task, file, testCmd)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"task_id": taskID,
		"status":  "RUNNING",
	})
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if s.handleCORSAndPreflight(w, r) {
		return
	}
	if !s.validateAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := filepath.Base(r.URL.Path)

	var state TaskState
	if err := s.db.First(&state, "id = ?", taskID).Error; err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
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
