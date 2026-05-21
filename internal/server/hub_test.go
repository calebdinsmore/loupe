package server

import (
	"testing"

	"github.com/calebjdinsmore/loupe/internal/agent"
)

// broadcast is the single stamping point: events arriving without a timestamp
// get one, the stamps advance monotonically, and pre-stamped events are left
// alone so replayed history keeps its original times.
func TestBroadcastStampsTimestamp(t *testing.T) {
	h := newHub()
	ch := h.subscribe()

	h.broadcast(agent.Event{Type: agent.EventText, Text: "first"})
	first := <-ch
	if first.Ts == 0 {
		t.Fatalf("expected broadcast to stamp Ts, got 0")
	}

	h.broadcast(agent.Event{Type: agent.EventText, Text: "second"})
	second := <-ch
	if second.Ts < first.Ts {
		t.Fatalf("expected monotonic Ts, got first=%d second=%d", first.Ts, second.Ts)
	}
}

func TestBroadcastPreservesExistingTimestamp(t *testing.T) {
	h := newHub()
	ch := h.subscribe()

	const stamped = int64(1234567890)
	h.broadcast(agent.Event{Type: agent.EventText, Text: "pre-stamped", Ts: stamped})
	ev := <-ch
	if ev.Ts != stamped {
		t.Fatalf("expected pre-stamped Ts %d to be preserved, got %d", stamped, ev.Ts)
	}
}
