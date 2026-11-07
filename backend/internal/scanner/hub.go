package scanner

import "sync"

// Event is pushed to SSE subscribers while a scan runs.
type Event struct {
	Type     string `json:"type"` // progress | done | error
	JobID    int64  `json:"jobId"`
	DomainID int64  `json:"domainId"`
	Stage    string `json:"stage,omitempty"`
	Tested   int    `json:"tested"`
	Total    int    `json:"total"`
	OK       int    `json:"ok"`
	Progress int    `json:"progress"`
	Msg      string `json:"msg,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Hub fans scan events out to connected SSE clients, keyed by domain id.
type Hub struct {
	mu   sync.RWMutex
	subs map[int64]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[int64]map[chan Event]struct{})}
}

func (h *Hub) Subscribe(domainID int64) (chan Event, func()) {
	ch := make(chan Event, 32)
	h.mu.Lock()
	if h.subs[domainID] == nil {
		h.subs[domainID] = make(map[chan Event]struct{})
	}
	h.subs[domainID][ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		if set, ok := h.subs[domainID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, domainID)
			}
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[ev.DomainID] {
		select {
		case ch <- ev:
		default: // drop for slow clients rather than blocking the scan
		}
	}
}
