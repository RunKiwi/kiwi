// ee/orchestrator/slack_thread_reply_test.go
package orchestrator

import (
	"context"
	"testing"
)

func TestClassifyThreadReplyContinue(t *testing.T) {
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"verdict": "continue"}`, nil
	}
	got, err := classifyThreadReply(context.Background(), complete, "PR #9 fixes the null check", "also handle the empty-string case")
	if err != nil || got != "continue" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestClassifyThreadReplyRejectsUnknownVerdict(t *testing.T) {
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"verdict": "something-else"}`, nil
	}
	if _, err := classifyThreadReply(context.Background(), complete, "summary", "message"); err == nil {
		t.Fatal("expected an error for an unrecognized verdict")
	}
}
