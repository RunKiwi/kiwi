package orchestrator

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func multipartTask(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("task", "fix")
	_ = mw.WriteField("file", "a.go")
	_ = mw.WriteField("test_cmd", "go test ./...")
	fw, _ := mw.CreateFormFile("codebase", "c.zip")
	// minimal valid zip
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("a.go")
	_, _ = w.Write([]byte("package a\n"))
	_ = zw.Close()
	_, _ = fw.Write(zbuf.Bytes())
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func postTask(t *testing.T, s *Server, key string) map[string]string {
	t.Helper()
	body, ctype := multipartTask(t)
	req := httptest.NewRequest(http.MethodPost, "/tasks", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("KIWI_SERVER_TOKEN"))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rw := httptest.NewRecorder()
	s.handleTasks(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rw.Body.Bytes(), &out)
	return out
}

func TestIdempotentSubmit(t *testing.T) {
	t.Setenv("KIWI_SERVER_TOKEN", "test-token")
	db := newTestDB(t)
	s := &Server{db: db}
	s.launchFn = func(taskID, sandboxPath, task, file, testCmd string) {} // no engine

	first := postTask(t, s, "dup-key")
	second := postTask(t, s, "dup-key")
	if first["task_id"] != second["task_id"] {
		t.Errorf("same key must dedupe: %s vs %s", first["task_id"], second["task_id"])
	}

	other := postTask(t, s, "different-key")
	if other["task_id"] == first["task_id"] {
		t.Errorf("different key must create a new task")
	}

	var count int64
	db.Model(&TaskState{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 rows (dup-key once + different-key), got %d", count)
	}
}
