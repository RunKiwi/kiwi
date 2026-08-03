package daemon

import (
	"testing"
	"time"
)

// daemonStaleAfter, restated from pkg/store/queue_diagnose.go.
//
// Duplicated rather than imported because pkg/daemon does not depend on
// pkg/store and should not start: the daemon is the customer-side binary and
// the store is the Control Plane's database layer. The coupling is real even so
// — this constant decides whether a busy daemon is advertised as offline — so
// it is written down here with a test rather than left as folklore.
const cpDaemonStaleAfter = 3 * time.Minute

// The renewal interval has to sit under two separate deadlines, and it used to
// respect only one.
//
// The lease is 10 minutes, so the old 4-minute interval kept the task fine. But
// the Control Plane reports a daemon offline after 3 minutes without contact,
// and a daemon runs its task synchronously on the poll goroutine — so it sends
// no heartbeat for the whole run, and renewal is its only remaining contact. At
// 4 minutes that contact was rarer than the staleness window, so a daemon doing
// exactly what it was asked went dark between renewals: "no runner is connected"
// on a queued sibling, and an offline badge on a runner that was working.
func TestRenewIntervalKeepsABusyDaemonVisible(t *testing.T) {
	if defaultRenewInterval >= cpDaemonStaleAfter {
		t.Fatalf("defaultRenewInterval (%s) must be shorter than the Control Plane's staleness window (%s), "+
			"or a daemon busy on a task is reported offline between renewals",
			defaultRenewInterval, cpDaemonStaleAfter)
	}
}

// leaseTTL, restated from pkg/orchestrator/daemon_api.go for the same reason.
const cpLeaseTTL = 10 * time.Minute

// The other deadline. Renewing must leave room for a transient failure or two:
// a network blip should cost a retry, not the task. This is what makes a long
// run survivable — at 2 minutes a lease gets five attempts rather than two,
// which matters more as the per-task wall-clock cap grows.
func TestRenewIntervalLeavesRoomForRetries(t *testing.T) {
	attempts := int(cpLeaseTTL / defaultRenewInterval)
	if attempts < 3 {
		t.Errorf("a lease should survive at least two failed renewals; %s into %s gives only %d attempts",
			defaultRenewInterval, cpLeaseTTL, attempts)
	}
}
