// ee/orchestrator/slack_completion_test.go
package orchestrator

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/slackapp"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type fakeSlackRecorder struct {
	messages []string
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func fakeSlackEdits(t *testing.T, s *Server) *fakeSlackRecorder {
	rec := &fakeSlackRecorder{}
	s.slackClient = slackapp.New(
		slackapp.WithBaseURL("https://slack.com/api"),
		slackapp.WithHTTPClient(&http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/chat.update") {
					var body map[string]interface{}
					_ = json.NewDecoder(r.Body).Decode(&body)
					if ts, ok := body["ts"].(string); ok {
						rec.messages = append(rec.messages, ts)
					}
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				}, nil
			}),
		}),
	)
	return rec
}

func TestReportSlackCompletionEditsStatusMessageOnSuccess(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SaveCredential(ctx, "org_1", "SLACK_BOT_TOKEN", store.CredentialSlack, "xoxb-test")
	_ = s.storage.CreateSlackTriggeredTask(ctx, &store.SlackTriggeredTask{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.001",
		QueuedTaskID: "task_1", StatusMessageTS: "100.002", LastStatus: "running",
	})

	edited := fakeSlackEdits(t, s)

	prURL := "https://github.com/acme/widget/pull/9"
	task := &store.QueuedTask{ID: "task_1", OrgID: "org_1", Status: store.TaskSucceeded, ResultURL: &prURL}
	s.reportSlackCompletion(ctx, "task_1", task)

	if len(edited.messages) != 1 || edited.messages[0] != "100.002" {
		t.Fatalf("expected exactly one edit to ts 100.002, got %v", edited.messages)
	}

	var row store.SlackTriggeredTask
	s.db.WithContext(ctx).Where("queued_task_id = ?", "task_1").First(&row)
	if row.LastStatus != "succeeded" {
		t.Fatalf("expected last_status updated to succeeded, got %q", row.LastStatus)
	}
}

func TestReportSlackCompletionNoOpsForATaskWithNoSlackOrigin(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()
	task := &store.QueuedTask{ID: "task_not_slack", OrgID: "org_1", Status: store.TaskSucceeded}
	s.reportSlackCompletion(ctx, "task_not_slack", task)
}
