package daemon

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The exact task that failed in production, copied from the queue row for
// job_25402621bd3c1f8e-worker-cookie-consent. 1370 characters.
const productionCookieConsentTask = `Add a cookie consent popup to the RunKiwi website. Steps:
1. Clone the repo and inspect the project structure (package.json, tech stack, existing styles/components).
2. Check if a cookie-consent library is already installed (e.g. react-cookie-consent, vanilla-cookieconsent, cookieconsent by Osano). If not, install the most suitable one — prefer ` + "`react-cookie-consent`" + ` for React/Next.js projects or ` + "`vanilla-cookieconsent`" + ` for plain JS/HTML sites.
3. Inspect the existing design tokens: colours (primary, background, text), font family, border-radius, button styles from CSS/Tailwind/SCSS files.
4. Implement the popup component/snippet that:
   - Appears fixed at the bottom (or bottom-centre) of the viewport on first visit.
   - Shows a short message like 'We use cookies to improve your experience.'
   - Has an 'Accept' button and optionally a 'Decline' or 'Learn more' link.
   - Stores the user's choice in localStorage or a cookie so it does not reappear after acceptance.
   - Matches the site's colour palette, typography, border-radius, and button styles exactly.
5. Wire the component into the app entry point (e.g. _app.tsx, App.jsx, index.html, or layout file) so it renders on every page.
6. Ensure no TypeScript or lint errors (run ` + "`npm run build`" + ` or ` + "`npm run lint`" + ` if scripts exist).
7. Commit all changes with message 'feat: add cookie consent popup'.`

// The production failure, reproduced against the real input.
//
// The task ran the whole loop, edited the repo, passed verification, committed
// and pushed a branch — then delivery failed with:
//
//	create PR: github api returned status 422
//
// which was GitHub saying "title is too long (maximum is 256 characters)". The
// title was "Kiwi: " + this entire 1370-character instruction block. Verified
// against the live API: a 281-character title is accepted and a 1376-character
// one is not, so this was never going to work for any planner-expanded task.
func TestPRTitle_ProductionTaskFitsGitHubsLimit(t *testing.T) {
	if len(productionCookieConsentTask) < 1000 {
		t.Fatalf("fixture no longer resembles the real task (%d chars)", len(productionCookieConsentTask))
	}

	title, body := prTitleAndBody(productionCookieConsentTask, "", "")

	if n := utf8.RuneCountInString(title); n > maxPRTitleLen {
		t.Errorf("title is %d characters; GitHub rejects anything over %d", n, maxPRTitleLen)
	}
	if strings.Contains(title, "\n") {
		t.Errorf("a title must be one line, got %q", title)
	}
	// It should be the summary the planner actually wrote, not a truncation of
	// step 1.
	if !strings.Contains(title, "Add a cookie consent popup to the RunKiwi website") {
		t.Errorf("title lost the summary line: %q", title)
	}
	// Nothing may be silently dropped — the instructions have to survive
	// somewhere a reviewer can read them.
	if !strings.Contains(body, "Wire the component into the app entry point") {
		t.Error("the full task description should be preserved in the body")
	}
}

func TestPRTitle_LimitsAndShape(t *testing.T) {
	cases := []struct {
		name string
		task string
		want func(t *testing.T, title, body string)
	}{
		{
			name: "short single-line task is untouched",
			task: "Fix division by zero in Divide()",
			want: func(t *testing.T, title, body string) {
				if title != "Kiwi: Fix division by zero in Divide()" {
					t.Errorf("got %q", title)
				}
			},
		},
		{
			name: "a first line longer than the cap is truncated with a marker",
			task: strings.Repeat("a", 400),
			want: func(t *testing.T, title, body string) {
				if utf8.RuneCountInString(title) != maxPRTitleLen {
					t.Errorf("got %d characters, want exactly %d", utf8.RuneCountInString(title), maxPRTitleLen)
				}
				if !strings.HasSuffix(title, "...") {
					t.Errorf("a truncated title should show it was cut: %q", title)
				}
			},
		},
		{
			name: "an empty task still yields a usable title",
			task: "   \n\n  ",
			want: func(t *testing.T, title, body string) {
				if title == "Kiwi: " || strings.TrimSpace(title) == "Kiwi:" {
					t.Errorf("title is empty of content: %q", title)
				}
			},
		},
		{
			name: "leading blank lines do not produce an empty title",
			task: "\n\nAdd a health check endpoint\nmore detail here",
			want: func(t *testing.T, title, body string) {
				if title != "Kiwi: Add a health check endpoint" {
					t.Errorf("got %q", title)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, body := prTitleAndBody(tc.task, "", "")
			if n := utf8.RuneCountInString(title); n > maxPRTitleLen {
				t.Errorf("title exceeds the cap at %d characters", n)
			}
			tc.want(t, title, body)
		})
	}
}

// The Architect writes Summary specifically to become this body (see its
// doc comment on session.Spec) — before this, the body was built from the
// raw task prompt regardless, which told a reviewer what was asked for but
// never what actually happened.
func TestPRTitle_SummaryDrivesTitleAndBody(t *testing.T) {
	title, body := prTitleAndBody(
		"Fix the login bug. Also add a regression test.",
		"Fixed a nil pointer dereference in the session middleware and added a regression test.",
		"",
	)

	if title != "Kiwi: Fixed a nil pointer dereference in the session middleware and added a regression test." {
		t.Errorf("title should come from the summary, got %q", title)
	}
	if !strings.Contains(body, "## Summary") {
		t.Errorf("body should have a Summary section, got %q", body)
	}
	if !strings.Contains(body, "Fixed a nil pointer dereference") {
		t.Errorf("body lost the summary text: %q", body)
	}
	if !strings.Contains(body, "<details>") || !strings.Contains(body, "Fix the login bug") {
		t.Errorf("the original task should still be preserved, collapsed, in the body: %q", body)
	}
}

// An empty summary (a checkpoint from an older daemon, or any path that never
// reached an approving review) must not produce an empty PR — same fallback
// behavior as before Summary existed.
func TestPRTitle_EmptySummaryFallsBackToTask(t *testing.T) {
	title, body := prTitleAndBody("Add a health check endpoint", "", "")

	if title != "Kiwi: Add a health check endpoint" {
		t.Errorf("got %q", title)
	}
	if !strings.Contains(body, "## Task") || strings.Contains(body, "## Summary") {
		t.Errorf("an empty summary should fall back to the plain task body, got %q", body)
	}
}

// The footer must never be a link that doesn't resolve — an empty taskURL
// (no KIWI_DASHBOARD_URL, or no job id) degrades to plain text instead.
func TestPRTitle_FooterLinksOnlyWhenATaskURLExists(t *testing.T) {
	_, withLink := prTitleAndBody("task", "summary", "https://app.runkiwi.dev/tasks/job_123")
	if !strings.Contains(withLink, "[Generated by Kiwi](https://app.runkiwi.dev/tasks/job_123)") {
		t.Errorf("expected a linked footer, got %q", withLink)
	}

	_, withoutLink := prTitleAndBody("task", "summary", "")
	if strings.Contains(withoutLink, "](") {
		t.Errorf("no taskURL means no link markup at all, got %q", withoutLink)
	}
	if !strings.Contains(withoutLink, "*Generated by Kiwi*") {
		t.Errorf("expected the plain-text footer, got %q", withoutLink)
	}
}

func TestTaskDrawerURL(t *testing.T) {
	t.Setenv("KIWI_DASHBOARD_URL", "https://app.runkiwi.dev/")
	if got, want := taskDrawerURL("job_123"), "https://app.runkiwi.dev/tasks/job_123"; got != want {
		t.Errorf("taskDrawerURL = %q, want %q (trailing slash on the base must not double up)", got, want)
	}

	t.Setenv("KIWI_DASHBOARD_URL", "")
	if got := taskDrawerURL("job_123"); got != "" {
		t.Errorf("no KIWI_DASHBOARD_URL should yield no URL at all, got %q", got)
	}

	t.Setenv("KIWI_DASHBOARD_URL", "https://app.runkiwi.dev")
	if got := taskDrawerURL(""); got != "" {
		t.Errorf("no job id should yield no URL even with a dashboard configured, got %q", got)
	}
}

// Truncation counts characters, not bytes: GitHub states the limit in
// characters, and cutting mid-rune would emit invalid UTF-8 into the API.
func TestPRTitle_MultiByteTruncationStaysValid(t *testing.T) {
	title, _ := prTitleAndBody(strings.Repeat("日", 400), "", "")

	if !utf8.ValidString(title) {
		t.Error("truncation split a multi-byte character")
	}
	if n := utf8.RuneCountInString(title); n > maxPRTitleLen {
		t.Errorf("got %d characters, want <= %d", n, maxPRTitleLen)
	}
}

// The body has its own limit, far larger, but a task carrying a pasted log
// should not push the request past it.
func TestPRBody_IsCapped(t *testing.T) {
	_, body := prTitleAndBody(strings.Repeat("x", 200000), "", "")

	if n := utf8.RuneCountInString(body); n > maxPRBodyLen {
		t.Errorf("body is %d characters, over the %d cap", n, maxPRBodyLen)
	}
}
