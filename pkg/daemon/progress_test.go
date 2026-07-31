package daemon

import (
	"strings"
	"sync"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// Each flush sends only what the Control Plane has not accepted. A run that
// re-sent its whole history every three seconds would grow quadratically in
// traffic for no added information.
func TestProgress_SendsOnlyTheDelta(t *testing.T) {
	p := &progressReporter{}
	p.add(ver.TaskEvent{Step: 0, Phase: "initial_test", Outcome: "fail"})
	p.add(ver.TaskEvent{Step: 1, Phase: "actor", Outcome: "proposed"})

	events, _, _, upto := p.pending()
	if len(events) != 2 {
		t.Fatalf("first flush should carry both events, got %d", len(events))
	}
	p.ack(upto)

	if events, _, _, _ := p.pending(); len(events) != 0 {
		t.Errorf("acknowledged events must not be re-sent, got %d", len(events))
	}

	p.add(ver.TaskEvent{Step: 1, Phase: "critic", Outcome: "rejected"})
	events, _, _, _ = p.pending()
	if len(events) != 1 || events[0].Phase != "critic" {
		t.Errorf("only the new event should be pending, got %+v", events)
	}
}

// A failed flush must not lose events. The delta stays pending until the
// Control Plane actually accepts it, so a network blip costs a delay and not a
// hole in the timeline.
func TestProgress_UnacknowledgedEventsSurviveAFailedFlush(t *testing.T) {
	p := &progressReporter{}
	p.add(ver.TaskEvent{Step: 1, Phase: "actor", Outcome: "proposed"})

	events, _, _, _ := p.pending() // flush "fails": no ack
	if len(events) != 1 {
		t.Fatalf("expected 1 pending event, got %d", len(events))
	}

	again, _, _, _ := p.pending()
	if len(again) != 1 {
		t.Errorf("an unacknowledged event must stay pending, got %d", len(again))
	}
}

// The final report is assembled from all(), not from what was streamed, so the
// authoritative history is complete regardless of which updates landed.
func TestProgress_AllReturnsTheFullHistory(t *testing.T) {
	p := &progressReporter{}
	for i := 0; i < 3; i++ {
		p.add(ver.TaskEvent{Step: i, Phase: "actor"})
	}
	_, _, _, upto := p.pending()
	p.ack(upto)

	if got := p.all(); len(got) != 3 {
		t.Errorf("all() = %d events, want the full 3 even after acking", len(got))
	}
}

// The output tail is bounded: it is sent every few seconds, and an npm install
// can emit megabytes. The END is kept, because that is the part that says what
// the command is doing now.
func TestProgress_OutputTailIsBoundedAndKeepsTheEnd(t *testing.T) {
	p := &progressReporter{}
	long := strings.Repeat("x", maxTailBytes*3) + "THE-END"
	p.setActivity("test: npm test", long)

	_, phase, tail, _ := p.pending()
	if phase != "test: npm test" {
		t.Errorf("phase = %q", phase)
	}
	if len(tail) > maxTailBytes {
		t.Errorf("tail is %d bytes, want <= %d", len(tail), maxTailBytes)
	}
	if !strings.HasSuffix(tail, "THE-END") {
		t.Error("the tail must keep the end of the output, which is the part that is current")
	}
}

// A nil reporter is the test path and any future caller that does not want
// progress. It must be inert rather than a panic in the middle of a real run.
func TestProgress_NilReporterIsSafe(t *testing.T) {
	var p *progressReporter
	p.add(ver.TaskEvent{Phase: "actor"})
	p.setActivity("test", "output")
	if got := p.all(); got != nil {
		t.Errorf("nil reporter should yield no events, got %+v", got)
	}
}

// add() is called inline on the loop goroutine while the flush ticker reads
// from another. The race detector proves the lock actually covers both.
func TestProgress_ConcurrentAddAndFlush(t *testing.T) {
	p := &progressReporter{}
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			p.add(ver.TaskEvent{Step: i, Phase: "actor"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _, _, upto := p.pending()
			p.ack(upto)
			p.setActivity("test", "chunk")
		}
	}()
	wg.Wait()

	if got := p.all(); len(got) != 200 {
		t.Errorf("recorded %d events, want 200", len(got))
	}
}
