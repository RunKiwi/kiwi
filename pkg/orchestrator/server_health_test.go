package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthEndpoints(t *testing.T) {
	db := newTestDB(t)
	srv := &Server{db: db}

	t.Run("healthz returns 200 OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()

		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status %d; got %d", http.StatusOK, w.Code)
		}
		if w.Body.String() != "OK" {
			t.Errorf("expected body OK; got %q", w.Body.String())
		}
	})

	t.Run("readyz returns 200 OK when DB is healthy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		mux := http.NewServeMux()
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			sqlDB, err := srv.db.DB()
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

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status %d; got %d", http.StatusOK, w.Code)
		}
		if w.Body.String() != "OK" {
			t.Errorf("expected body OK; got %q", w.Body.String())
		}
	})

	t.Run("readyz returns 503 when DB is closed", func(t *testing.T) {
		sqlDB, _ := srv.db.DB()
		sqlDB.Close()

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		mux := http.NewServeMux()
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			sqlDB, err := srv.db.DB()
			if err != nil {
				http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
			defer cancel()
			if err := sqlDB.PingContext(ctx); err != nil {
				http.Error(w, "Database ping failed", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status %d; got %d", http.StatusServiceUnavailable, w.Code)
		}
	})
}
