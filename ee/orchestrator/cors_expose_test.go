// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The record hash is returned in a header, and a browser can read only the
// response headers a server names in Access-Control-Expose-Headers. The
// dashboard is a different origin to the API, so an unexposed header is sent
// and then dropped before any JavaScript sees it — the receipt panel showed "—"
// for a record that has a hash.
//
// There are two CORS middlewares in this server, and /api/v1/* passes through
// the second one. Fixing only the first is exactly the mistake this test
// exists to catch: it verifies the header on the route that actually serves
// the record, not on whichever one happened to be edited.
func TestCORS_ExposesRecordHashOnAPIRoutes(t *testing.T) {
	t.Setenv("KIWI_CORS_ALLOWED_ORIGINS", "https://app.runkiwi.dev")

	for _, path := range []string{
		"/api/v1/jobs/job_1/record",
		"/api/v1/jobs",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodOptions, path, nil)
			req.Header.Set("Origin", "https://app.runkiwi.dev")
			req.Header.Set("Access-Control-Request-Method", "GET")

			corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(rec, req)

			exposed := rec.Header().Get("Access-Control-Expose-Headers")
			if !strings.Contains(exposed, "X-Kiwi-Record-Hash") {
				t.Errorf("Access-Control-Expose-Headers = %q; a browser cannot read the record hash without it", exposed)
			}
		})
	}
}
