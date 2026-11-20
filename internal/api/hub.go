package api

import "sync"

// Event names a job that changed.
type Event struct {
	Flow  string `json:"flow"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Key   string `json:"key"`
}

// Hub fans a change out to every connected stream.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: map[chan Event]struct{}{}}
}

// Publish sends one change to every subscriber. A slow subscriber drops it;
// the next job fetch or a reconnect carries the current state.
func (h *Hub) Publish(flow, owner, repo, key string) {
	event := Event{Flow: flow, Owner: owner, Repo: repo, Key: key}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *Hub) subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}
