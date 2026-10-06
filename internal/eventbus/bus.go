package eventbus

import (
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

type Handler func(types.Event)

type Bus struct {
	mu     sync.RWMutex
	subs   map[int]subscription
	next   int
	closed bool
	now    func() time.Time
}

type subscription struct {
	filter string
	box    *mailbox
}

// maxQueue bounds what one subscriber may fall behind by; past it the oldest
// events go, so a stalled reader cannot hold the daemon's memory.
const maxQueue = 8192

// mailbox delivers one subscriber's events in the order they were published,
// on a goroutine of its own. A goroutine per event (as before) let a reply's
// text arrive out of order: streamed words came through shuffled.
type mailbox struct {
	h     Handler
	mu    sync.Mutex
	items []types.Event
	wake  chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newMailbox(h Handler) *mailbox {
	m := &mailbox{h: h, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go m.run()
	return m
}

func (m *mailbox) push(ev types.Event) {
	m.mu.Lock()
	if len(m.items) >= maxQueue {
		m.items = m.items[1:]
	}
	m.items = append(m.items, ev)
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *mailbox) run() {
	for {
		select {
		case <-m.done:
			return
		case <-m.wake:
		}
		for {
			m.mu.Lock()
			batch := m.items
			m.items = nil
			m.mu.Unlock()
			if len(batch) == 0 {
				break
			}
			for _, ev := range batch {
				select {
				case <-m.done:
					return
				default:
				}
				m.h(ev)
			}
		}
	}
}

func (m *mailbox) stop() { m.once.Do(func() { close(m.done) }) }

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
			s.box.push(ev)
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
	box := newMailbox(h)
	b.subs[id] = subscription{filter: filter, box: box}
	return func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
		box.stop()
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	b.closed = true
	for _, s := range b.subs {
		s.box.stop()
	}
	b.subs = map[int]subscription{}
	b.mu.Unlock()
}
