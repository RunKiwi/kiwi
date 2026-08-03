package daemon

import (
	"context"
	"encoding/base64"
	"log"
	"sync"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// Live progress reporting.
//
// The daemon runs the Actor–Critic loop in its own process, so it is the only
// thing that can see a run happen. Until now it kept everything to itself and
// posted the whole story once, with the result — which meant a task that had
// been running for ten minutes showed a spinner and nothing else, and a task
// that was stuck looked identical to one working hard.
//
// The loop already emits every phase as it happens (loop.Config.OnEvent). What
// was missing was a way to get those out of the process before the end. This
// buffers them and flushes to the Control Plane on a short ticker.
//
// Deliberately NOT carried on the lease-renewal request, which is the other
// thing already talking to the CP during a run. Renewal is what keeps the task
// alive: a progress payload that failed — too large, a transient 500 — would
// take the renewal down with it and the daemon would lose a task it was
// successfully working on. Progress is best-effort and must never be able to do
// that, so it gets its own call, and every error here is logged and dropped.

// defaultProgressInterval is how often progress reaches the dashboard. Short
// enough that "what is it doing right now" is answered honestly, long enough
// that a fleet of daemons is not a load generator. The lease renewal, by
// contrast, runs on minutes — far too slow to watch a run by.
const defaultProgressInterval = 3 * time.Second

// maxTailBytes bounds the command output carried with each update. The end of a
// running command is the part that says what it is doing; the beginning is
// usually a banner. Bounded because this is sent every few seconds and an npm
// install can emit megabytes.
const maxTailBytes = 4000

// progressReporter accumulates what has happened so far and tracks how much of
// it the Control Plane has accepted, so each flush sends only the delta.
type progressReporter struct {
	mu     sync.Mutex
	events []ver.TaskEvent
	// sent is how many leading events the CP has acknowledged. A failed flush
	// leaves it alone, so the next tick retries the same delta rather than
	// dropping it.
	sent  int
	tail  string
	phase string
}

// add records one loop phase. Called inline on the loop goroutine.
//
// The timestamp is taken here rather than where the row is persisted. The
// Control Plane writes a whole flush at once, so stamping it there gave every
// event in a three-second batch the same instant — which is exactly the
// resolution needed to see where a run spends time it does not account for.
func (p *progressReporter) add(ev ver.TaskEvent) {
	if p == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

// setActivity records what is running now and the tail of its output, so a long
// command reports something other than silence.
func (p *progressReporter) setActivity(phase, output string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = phase
	p.tail = outputTail(output, maxTailBytes)
}

// pending returns the events not yet accepted, plus the current activity.
func (p *progressReporter) pending() (events []ver.TaskEvent, phase, tail string, upto int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	upto = len(p.events)
	if p.sent < upto {
		events = append(events, p.events[p.sent:upto]...)
	}
	return events, p.phase, p.tail, upto
}

// ack marks everything below upto as accepted by the Control Plane.
func (p *progressReporter) ack(upto int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if upto > p.sent {
		p.sent = upto
	}
}

// all returns every event recorded, for the authoritative final report.
func (p *progressReporter) all() []ver.TaskEvent {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ver.TaskEvent(nil), p.events...)
}

// streamProgress flushes progress until ctx ends. It returns when the task is
// over; the final, authoritative event list still goes with ReportResult, which
// replaces whatever was streamed.
func (d *Daemon) streamProgress(ctx context.Context, taskID, leaseID string, p *progressReporter) {
	interval := d.config.ProgressInterval
	if interval <= 0 {
		interval = defaultProgressInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, phase, tail, upto := p.pending()
			// Nothing new to say. An unchanged phase with no new events is the
			// common case between steps, and posting it would be pure noise.
			if len(events) == 0 && phase == "" {
				continue
			}
			// A cancelled task must not have its shutdown delayed by a POST, so
			// this uses the task context: when the run ends, the flush stops.
			if err := d.client.ReportProgress(ctx, ProgressReq{
				TaskID:     taskID,
				LeaseID:    leaseID,
				SignPubKey: d.signPubKeyB64(),
				Events:     events,
				Phase:      phase,
				OutputTail: tail,
			}); err != nil {
				// Best-effort by design: telemetry must never affect a run. The
				// unsent delta stays pending and the next tick retries it.
				log.Printf("Task %s: progress update failed (will retry): %v", taskID, err)
				continue
			}
			p.ack(upto)
		}
	}
}

// signPubKeyB64 is the daemon's signing identity in the form every request
// carries it.
func (d *Daemon) signPubKeyB64() string {
	return base64.StdEncoding.EncodeToString(d.signPubKey)
}
