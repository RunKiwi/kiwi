package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitTaskSendsIdempotencyKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		_, _ = w.Write([]byte(`{"task_id":"x","status":"RUNNING"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	c.IdempotencyKey = "my-key"
	if _, err := c.SubmitTask(context.Background(), "t", "f", "cmd", []byte("z")); err != nil {
		t.Fatal(err)
	}
	if gotKey != "my-key" {
		t.Errorf("header: got %q want my-key", gotKey)
	}
}

func TestSubmitTaskOmitsEmptyIdempotencyKey(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["Idempotency-Key"]
		_, _ = w.Write([]byte(`{"task_id":"x","status":"RUNNING"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok") // no key set
	if _, err := c.SubmitTask(context.Background(), "t", "f", "cmd", []byte("z")); err != nil {
		t.Fatal(err)
	}
	if present {
		t.Errorf("Idempotency-Key header should be absent when unset")
	}
}
