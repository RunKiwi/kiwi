package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A task reached delivery and reported, in full:
//
//	publish failed: create PR: github api returned status 422
//
// That is every fact we had. 422 on this endpoint covers at least four
// different faults with four different fixes — a base branch that does not
// exist, a head branch that does not exist, a pull request that already exists,
// and an empty diff — and the status alone distinguishes none of them. GitHub
// names the failing field in the response body every time; we read the body
// only on the already-exists path and discarded it everywhere else.
//
// The bodies below are captured verbatim from the live API, not invented.
func TestCreatePR_ErrorNamesTheActualGitHubFault(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "base branch does not exist",
			body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"base","code":"invalid"}],"documentation_url":"https://docs.github.com/rest","status":"422"}`,
			want: []string{"Validation Failed", "base is invalid"},
		},
		{
			name: "head branch does not exist",
			body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"head","code":"invalid"}],"status":"422"}`,
			want: []string{"head is invalid"},
		},
		{
			name: "nothing to merge",
			body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"No commits between main and main"}],"status":"422"}`,
			want: []string{"No commits between main and main"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/o/r/pulls" && r.Method == http.MethodGet {
					// No open PR for this head, so CreatePR cannot adopt one and
					// must surface the real error.
					w.Write([]byte(`[]`))
					return
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			gh := &restGitHub{token: "t", api: srv.URL}
			_, err := gh.CreatePR(context.Background(), "o", "r", "main", "kiwi/job_1", "title", "body")
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not explain the fault.\n got: %s\nwant substring: %q", err, want)
				}
			}
			// The branch names are what make the message actionable — knowing
			// "base is invalid" is no help without knowing which base.
			if !strings.Contains(err.Error(), "kiwi/job_1") || !strings.Contains(err.Error(), "main") {
				t.Errorf("error should name the branches involved, got: %s", err)
			}
		})
	}
}

// The one 422 that is not a failure: another worker of the same job already
// opened the PR (#126). This must keep returning that PR, not the new error.
func TestCreatePR_AdoptsTheExistingPRRatherThanReportingTheError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`[{"html_url":"https://github.com/o/r/pull/7"}]`))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"A pull request already exists for o:kiwi/job_1."}]}`))
	}))
	defer srv.Close()

	gh := &restGitHub{token: "t", api: srv.URL}
	url, err := gh.CreatePR(context.Background(), "o", "r", "main", "kiwi/job_1", "title", "body")
	if err != nil {
		t.Fatalf("an existing PR should be adopted, got error: %v", err)
	}
	if url != "https://github.com/o/r/pull/7" {
		t.Errorf("got %q, want the existing PR", url)
	}
}

// GitHub serves HTML for 5xx and for some proxy failures. A body that is not
// JSON must still reach the operator rather than being swallowed into "status
// 502" — but it must not paste an entire error page into a task result.
func TestCreatePR_NonJSONBodyIsPassedThroughTruncated(t *testing.T) {
	long := "<html><body>" + strings.Repeat("upstream connect error ", 200) + "</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(long))
	}))
	defer srv.Close()

	gh := &restGitHub{token: "t", api: srv.URL}
	_, err := gh.CreatePR(context.Background(), "o", "r", "main", "kiwi/job_1", "title", "body")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "upstream connect error") {
		t.Errorf("the body should survive, got: %s", err)
	}
	if len(err.Error()) > 800 {
		t.Errorf("an error page should not be pasted whole into a task result; got %d chars", len(err.Error()))
	}
}

func TestDescribeGitHubError_EmptyBody(t *testing.T) {
	if got := describeGitHubError(nil); !strings.Contains(got, "empty") {
		t.Errorf("got %q, want a note that the body was empty", got)
	}
}

// A message with no errors array (rate limits, permissions) still reads well.
func TestDescribeGitHubError_MessageOnly(t *testing.T) {
	got := describeGitHubError([]byte(`{"message":"Resource not accessible by integration"}`))
	if got != "Resource not accessible by integration" {
		t.Errorf("got %q", got)
	}
}
