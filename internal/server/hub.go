package server

import (
	"sync"
	"time"

	"github.com/calebjdinsmore/loupe/internal/agent"
)

// hub fans agent events out to WebSocket subscribers and replays history to
// late joiners, so reloading the page restores the full transcript.
type hub struct {
	mu      sync.Mutex
	subs    map[chan agent.Event]struct{}
	history []agent.Event
	running bool // an agent turn holds the slot; submits and messages cannot overlap
}

func newHub() *hub {
	return &hub{subs: make(map[chan agent.Event]struct{})}
}

// tryStart acquires the busy slot, returning false if a turn is already running.
func (h *hub) tryStart() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.running = true
	return true
}

// finish releases the busy slot.
func (h *hub) finish() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
}

func (h *hub) subscribe() chan agent.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan agent.Event, 256)
	for _, ev := range h.history {
		ch <- ev // buffer is generous; history precedes any live event
	}
	h.subs[ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(ch chan agent.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

func (h *hub) broadcast(ev agent.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.Ts == 0 {
		ev.Ts = time.Now().UnixMilli()
	}
	h.history = append(h.history, ev)
	for ch := range h.subs {
		select {
		case ch <- ev:
		default: // drop for a slow consumer rather than block the agent
		}
	}
}
