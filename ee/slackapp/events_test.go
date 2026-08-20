// ee/slackapp/events_test.go
package slackapp

import "testing"

func TestParseEventHandlesURLVerification(t *testing.T) {
	body := []byte(`{"type":"url_verification","challenge":"abc123"}`)
	ev, ok := ParseEvent(body)
	if !ok || ev.Type != "url_verification" || ev.Challenge != "abc123" {
		t.Fatalf("got %+v, ok=%v", ev, ok)
	}
}

func TestParseEventExtractsAppMention(t *testing.T) {
	body := []byte(`{
		"type": "event_callback",
		"team_id": "T123",
		"event": {
			"type": "app_mention",
			"user": "U1",
			"text": "<@U0BOT> fix this bug",
			"channel": "C1",
			"ts": "100.001",
			"thread_ts": "99.000"
		}
	}`)
	ev, ok := ParseEvent(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.TeamID != "T123" || ev.EventType != "app_mention" || ev.ChannelID != "C1" || ev.TS != "100.001" || ev.ThreadTS != "99.000" || ev.UserID != "U1" {
		t.Fatalf("got %+v", ev)
	}
}

func TestParseEventRejectsUnrecognizedShape(t *testing.T) {
	if _, ok := ParseEvent([]byte(`{"not":"slack"}`)); ok {
		t.Fatal("expected ok=false for an unrecognized payload")
	}
}

func TestParseInteractivityExtractsBlockActions(t *testing.T) {
	form := []byte(`payload=` + urlEncodedTestPayload)
	in, ok := ParseInteractivity(form)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if in.Type != "block_actions" || in.TeamID != "T123" || in.ChannelID != "C1" || in.ActionID != "fork" {
		t.Fatalf("got %+v", in)
	}
}

// urlEncodedTestPayload is url.QueryEscape of:
// {"type":"block_actions","team":{"id":"T123"},"channel":{"id":"C1"},
//
//	"message":{"ts":"100.001"},"user":{"id":"U1"},
//	"actions":[{"action_id":"fork","value":"stt_abc"}]}
const urlEncodedTestPayload = `%7B%22type%22%3A%22block_actions%22%2C%22team%22%3A%7B%22id%22%3A%22T123%22%7D%2C%22channel%22%3A%7B%22id%22%3A%22C1%22%7D%2C%22message%22%3A%7B%22ts%22%3A%22100.001%22%7D%2C%22user%22%3A%7B%22id%22%3A%22U1%22%7D%2C%22actions%22%3A%5B%7B%22action_id%22%3A%22fork%22%2C%22value%22%3A%22stt_abc%22%7D%5D%7D`
