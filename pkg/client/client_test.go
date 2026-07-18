package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubmitTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header: got %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("task") != "fix it" || r.FormValue("file") != "a.go" || r.FormValue("test_cmd") != "go test ./..." {
			t.Errorf("form values wrong: %v", r.MultipartForm.Value)
		}
		f, _, err := r.FormFile("codebase")
		if err != nil {
			t.Fatalf("codebase file: %v", err)
		}
		b, _ := io.ReadAll(f)
		if string(b) != "ZIPBYTES" {
			t.Errorf("codebase content: got %q", string(b))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"abc123","status":"RUNNING"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	id, err := c.SubmitTask(context.Background(), "fix it", "a.go", "go test ./...", []byte("ZIPBYTES"))
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	if id != "abc123" {
		t.Errorf("task id: got %q want abc123", id)
	}
}

func TestGetStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/abc123" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"abc123","status":"SUCCESS","logs":"done","cost":0.42}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	st, err := c.GetStatus(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.Status != "SUCCESS" || st.Logs != "done" || st.Cost != 0.42 {
		t.Errorf("status decode wrong: %+v", st)
	}
}

func TestGetStatusAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "bad")
	_, err := c.GetStatus(context.Background(), "abc123")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestDownloadResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("download") != "true" {
			t.Errorf("missing download=true: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("ZIPRESULT"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	b, err := c.DownloadResult(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("DownloadResult: %v", err)
	}
	if string(b) != "ZIPRESULT" {
		t.Errorf("result: got %q", string(b))
	}
}

func TestSetCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/credentials" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header: got %q", got)
		}
		var req map[string]string
		importJson := true
		_ = importJson // avoid unused var if json is already imported
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req["name"] != "FOO" || req["kind"] != "bar" || req["value"] != "baz" {
			t.Errorf("wrong payload: %v", req)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	err := c.SetCredential(context.Background(), "FOO", "bar", "baz")
	if err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
}
