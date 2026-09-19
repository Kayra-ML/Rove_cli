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
	unsub := b.Subscribe(string(types.EventCardUpdated), func(ev types.Event) { ch <- ev })
	defer unsub()
	b.Publish(types.Event{Type: types.EventCardUpdated, Payload: map[string]any{"id": "1"}})
	select {
	case ev := <-ch:
		if ev.Type != types.EventCardUpdated {
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
	unsub := b.Subscribe("card.*", func(ev types.Event) {
		mu.Lock()
		got = append(got, ev.Type)
		mu.Unlock()
	})
	defer unsub()
	b.Publish(types.Event{Type: types.EventCardUpdated})
	b.Publish(types.Event{Type: types.EventCardMoved})
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
		t.Fatalf("expected 2 card events, got %v", got)
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
