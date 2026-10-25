package hub

import "sync"

type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{}
}

func New() *Hub {
	return &Hub{subs: map[string]map[chan []byte]struct{}{}}
}

func (h *Hub) Subscribe(channel string) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	set := h.subs[channel]
	if set == nil {
		set = map[chan []byte]struct{}{}
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

func (h *Hub) Publish(channel string, payload []byte) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for ch := range h.subs[channel] {
		select {
		case ch <- payload:
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
