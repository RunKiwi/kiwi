package provisioner

import (
	"strings"
	"testing"
)

// firstLine feeds the error text persisted on a failed provisioning request and
// shown to the user, so it must extract the actionable message and stay bounded.
func TestFirstLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "keeps docker's actionable first line",
			in:   "Unable to find image 'kiwidaemon:latest' locally\ndocker: Error response from daemon: manifest unknown.\nSee 'docker run --help'.",
			want: "Unable to find image 'kiwidaemon:latest' locally",
		},
		{"single line", "docker: permission denied", "docker: permission denied"},
		{"empty output is still explanatory", "   \n  ", "no output"},
		{"trims surrounding whitespace", "\n  boom  \n", "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine([]byte(tt.in)); got != tt.want {
				t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A pathological failure must not balloon the persisted error text.
func TestFirstLineTruncates(t *testing.T) {
	got := firstLine([]byte(strings.Repeat("x", 5000)))
	if len(got) > 320 {
		t.Errorf("firstLine should cap its output, got %d chars", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated message should be marked as such, got %q", got[len(got)-10:])
	}
}

// A configured (registry) image must be pulled on every launch. `docker run`
// reuses a cached tag without consulting the registry, so with a moving tag like
// :latest a host that has launched once keeps starting the OLD daemon image —
// a deploy that appears to succeed and silently changes nothing.
func TestDockerLauncherPullsConfiguredImage(t *testing.T) {
	t.Setenv("KIWI_DAEMON_IMAGE", "us-central1-docker.pkg.dev/proj/repo/kiwidaemon:latest")

	d := NewDockerLauncher()
	if !d.pullAlways {
		t.Error("an explicitly configured registry image should be pulled on every launch")
	}
	if d.image != "us-central1-docker.pkg.dev/proj/repo/kiwidaemon:latest" {
		t.Errorf("image = %q", d.image)
	}
}

// The default image is built locally and never pushed, so forcing a pull would
// fail every launch trying to reach Docker Hub.
func TestDockerLauncherDoesNotPullLocalDefault(t *testing.T) {
	t.Setenv("KIWI_DAEMON_IMAGE", "")

	d := NewDockerLauncher()
	if d.pullAlways {
		t.Error("the local default image must not be pulled — it exists only on the host")
	}
	if d.image != defaultDaemonImage {
		t.Errorf("image = %q, want %q", d.image, defaultDaemonImage)
	}
}
