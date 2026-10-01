package service

import "sync"

// Event is a change notification pushed to connected clients over WebSocket.
type Event struct {
	Type     string `json:"type"`
	Path     string `json:"path,omitempty"`
	ID       string `json:"id,omitempty"`
	Status   string `json:"status,omitempty"`
	Revision int64  `json:"revision,omitempty"`
	Notes    int    `json:"notes,omitempty"`
	TookMS   int64  `json:"took_ms,omitempty"`
}

// Bus is a minimal fan-out for change events. Slow subscribers are skipped
// rather than allowed to block indexing.
type Bus struct {
	mu   sync.RWMutex
	subs map[int]chan Event
	next int
}

func NewBus() *Bus { return &Bus{subs: map[int]chan Event{}} }

// Subscribe returns a channel of events and a function to release it.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, 64)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default: // subscriber is behind; drop rather than stall the indexer
		}
	}
}
