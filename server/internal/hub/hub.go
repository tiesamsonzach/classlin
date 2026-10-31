package hub

import "sync"

// Packet is one SSE payload with an event name (e.g. "message", "signal").
type Packet struct {
	Event string
	Data  []byte
}

type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Packet]struct{}
}

func New() *Hub {
	return &Hub{subs: map[string]map[chan Packet]struct{}{}}
}

func (h *Hub) Subscribe(channel string) (<-chan Packet, func()) {
	ch := make(chan Packet, 16)
	h.mu.Lock()
	set := h.subs[channel]
	if set == nil {
		set = map[chan Packet]struct{}{}
		h.subs[channel] = set
	}
	set[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if set, ok := h.subs[channel]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(h.subs, channel)
				}
			}
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish sends a classroom message event.
func (h *Hub) Publish(channel string, payload []byte) int {
	return h.PublishEvent(channel, "message", payload)
}

// PublishEvent fans out a named SSE event to channel subscribers.
func (h *Hub) PublishEvent(channel, event string, payload []byte) int {
	if event == "" {
		event = "message"
	}
	pkt := Packet{Event: event, Data: payload}
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for ch := range h.subs[channel] {
		select {
		case ch <- pkt:
			n++
		default:
		}
	}
	return n
}

func (h *Hub) Count(channel string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[channel])
}
