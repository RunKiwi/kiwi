package main

import "testing"

// envFloat backs -session-budget's default. It matters more than a normal flag
// default because the free-tier provisioner launches per-org daemons with a
// fixed argv (-api-url and -cache-dir only), so on the fleet the environment is
// the only way to reach this setting at all.
func TestEnvFloat(t *testing.T) {
	const def = 5.00

	tests := []struct {
		name string
		set  bool
		val  string
		want float64
	}{
		{name: "unset uses the default", set: false, want: def},
		{name: "empty uses the default", set: true, val: "", want: def},
		{name: "a plain value is used", set: true, val: "12.5", want: 12.5},
		{name: "an integer value is used", set: true, val: "3", want: 3},

		// A daemon that refuses to boot is worse than one running the documented
		// default, so bad input degrades rather than fails.
		{name: "garbage falls back", set: true, val: "not-a-number", want: def},
		{name: "zero falls back", set: true, val: "0", want: def},
		{name: "negative falls back", set: true, val: "-1", want: def},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const key = "KIWI_TEST_ENV_FLOAT"
			if tc.set {
				t.Setenv(key, tc.val)
			}
			if got := envFloat(key, def); got != tc.want {
				t.Errorf("envFloat(%q=%q) = %v, want %v", key, tc.val, got, tc.want)
			}
		})
	}
}
