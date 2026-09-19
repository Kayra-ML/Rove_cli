package eventbus

import (
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

type Handler func(types.Event)

type Bus struct {
	mu       sync.RWMutex
	subs     map[int]subscription
	next     int
	closed   bool
	now      func() time.Time
}

type subscription struct {
	filter string
	h      Handler
}

func New() *Bus {
	return &Bus{
		subs: make(map[int]subscription),
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (b *Bus) Publish(ev types.Event) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = b.now()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, s := range b.subs {
		if s.filter == "" || s.filter == string(ev.Type) || s.filter == ev.Topic || matchPrefix(s.filter, ev) {
			h := s.h
			e := ev
			go h(e)
		}
	}
}

func matchPrefix(filter string, ev types.Event) bool {
	if len(filter) == 0 {
		return true
	}
	if filter[len(filter)-1] == '*' {
		p := filter[:len(filter)-1]
		return len(string(ev.Type)) >= len(p) && string(ev.Type)[:len(p)] == p
	}
	return false
}

func (b *Bus) Subscribe(filter string, h Handler) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	b.subs[id] = subscription{filter: filter, h: h}
	return func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	b.closed = true
	b.subs = map[int]subscription{}
	b.mu.Unlock()
}
