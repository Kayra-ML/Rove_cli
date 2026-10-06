package eventbus

import (
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	defer b.Close()
	ch := make(chan types.Event, 1)
	unsub := b.Subscribe(string(types.EventGoalUpdated), func(ev types.Event) { ch <- ev })
	defer unsub()
	b.Publish(types.Event{Type: types.EventGoalUpdated, Payload: map[string]any{"id": "1"}})
	select {
	case ev := <-ch:
		if ev.Type != types.EventGoalUpdated {
			t.Fatalf("got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestFilterWildcard(t *testing.T) {
	b := New()
	defer b.Close()
	var mu sync.Mutex
	var got []types.EventType
	unsub := b.Subscribe("tool.*", func(ev types.Event) {
		mu.Lock()
		got = append(got, ev.Type)
		mu.Unlock()
	})
	defer unsub()
	b.Publish(types.Event{Type: types.EventToolStart})
	b.Publish(types.Event{Type: types.EventToolResult})
	b.Publish(types.Event{Type: types.EventLog})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("expected 2 tool events, got %v", got)
	}
}

func TestUnsubscribe(t *testing.T) {
	b := New()
	defer b.Close()
	ch := make(chan types.Event, 2)
	unsub := b.Subscribe("", func(ev types.Event) { ch <- ev })
	unsub()
	b.Publish(types.Event{Type: types.EventLog})
	select {
	case <-ch:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestClosedDrops(t *testing.T) {
	b := New()
	ch := make(chan types.Event, 1)
	b.Subscribe("", func(ev types.Event) { ch <- ev })
	b.Close()
	b.Publish(types.Event{Type: types.EventLog})
	select {
	case <-ch:
		t.Fatal("closed bus should drop")
	case <-time.After(50 * time.Millisecond):
	}
}

// A reply streams as many small events; each subscriber must get them in
// the order they were published, or the text arrives shuffled.
func TestDeliversInOrder(t *testing.T) {
	b := New()
	defer b.Close()
	const n = 2000
	got := make(chan int, n)
	unsub := b.Subscribe("message.delta", func(ev types.Event) { got <- ev.Payload["i"].(int) })
	defer unsub()
	for i := 0; i < n; i++ {
		b.Publish(types.Event{Type: "message.delta", Payload: map[string]any{"i": i}})
	}
	for i := 0; i < n; i++ {
		select {
		case v := <-got:
			if v != i {
				t.Fatalf("event %d arrived as %d", i, v)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout after %d events", i)
		}
	}
}
