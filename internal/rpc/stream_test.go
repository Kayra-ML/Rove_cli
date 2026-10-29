package rpc

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
)

type sseFrame struct{ event, id, data string }

// sseReader reads frames off a stream: id/event/data lines up to a blank
// line; comments are skipped.
type sseReader struct {
	br   *bufio.Reader
	body interface{ Close() error }
}

func openStream(t *testing.T, base, token string, q url.Values) *sseReader {
	t.Helper()
	if q == nil {
		q = url.Values{}
	}
	q.Set("token", token)
	resp, err := http.Get(base + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	r := &sseReader{br: bufio.NewReader(resp.Body), body: resp.Body}
	t.Cleanup(func() { r.body.Close() })
	return r
}

func (r *sseReader) next(t *testing.T) sseFrame {
	t.Helper()
	got := make(chan sseFrame, 1)
	go func() {
		var f sseFrame
		for {
			line, err := r.br.ReadString('\n')
			if err != nil {
				got <- sseFrame{event: "eof"}
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case line == "":
				if f.data != "" || f.event != "" {
					got <- f
					return
				}
			case strings.HasPrefix(line, ":"):
			case strings.HasPrefix(line, "event: "):
				f.event = line[len("event: "):]
			case strings.HasPrefix(line, "id: "):
				f.id = line[len("id: "):]
			case strings.HasPrefix(line, "data: "):
				f.data = line[len("data: "):]
			}
		}
	}()
	select {
	case f := <-got:
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame")
		return sseFrame{}
	}
}

// nextEvent skips pings and returns the next event's type and frame id.
func (r *sseReader) nextEvent(t *testing.T) (string, string) {
	t.Helper()
	for {
		f := r.next(t)
		if f.event == "ping" {
			continue
		}
		if f.event != "" {
			t.Fatalf("unexpected %q frame: %s", f.event, f.data)
		}
		var ev types.Event
		if err := json.Unmarshal([]byte(f.data), &ev); err != nil {
			t.Fatalf("bad data %q", f.data)
		}
		return string(ev.Type), f.id
	}
}

func (r *sseReader) hello(t *testing.T) (resumed bool, seq uint64) {
	t.Helper()
	f := r.next(t)
	if f.event != "hello" {
		t.Fatalf("first frame = %+v", f)
	}
	var h struct {
		Resumed bool   `json:"resumed"`
		Seq     uint64 `json:"seq"`
		Epoch   string `json:"epoch"`
	}
	if err := json.Unmarshal([]byte(f.data), &h); err != nil || h.Epoch == "" {
		t.Fatalf("hello = %q", f.data)
	}
	return h.Resumed, h.Seq
}

func streamRig(t *testing.T) (*core.App, *Server, string) {
	t.Helper()
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	s := New(app)
	srv := httptest.NewServer(http.HandlerFunc(s.handleSSE))
	t.Cleanup(srv.Close)
	return app, s, srv.URL
}

// publish puts events on the bus and waits until the stream hub has
// numbered them all (the bus hands them over on a goroutine of its own).
func publish(t *testing.T, app *core.App, s *Server, types_ ...string) {
	t.Helper()
	want, _ := s.hub.current()
	want += uint64(len(types_))
	for _, ty := range types_ {
		app.Bus.Publish(types.Event{Type: types.EventType(ty), Topic: "test"})
	}
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if cur, _ := s.hub.current(); cur >= want {
			return
		}
		if time.Now().After(end) {
			t.Fatal("events never reached the stream hub")
		}
	}
}

// A client that comes back with the last id it saw gets what it missed —
// each event once, in order — and then the live ones.
func TestStreamResumesWhereTheClientWas(t *testing.T) {
	app, s, base := streamRig(t)
	a := openStream(t, base, app.Token, nil)
	if resumed, _ := a.hello(t); resumed {
		t.Fatal("a first stream cannot have resumed")
	}
	publish(t, app, s, "t.one", "t.two", "t.three")
	var last string
	for _, want := range []string{"t.one", "t.two", "t.three"} {
		ty, id := a.nextEvent(t)
		if ty != want {
			t.Fatalf("got %s, want %s", ty, want)
		}
		last = id
	}
	a.body.Close()
	publish(t, app, s, "t.four", "t.five")

	b := openStream(t, base, app.Token, url.Values{"since": {last}})
	if resumed, _ := b.hello(t); !resumed {
		t.Fatal("the gap is in the ring; the stream should resume")
	}
	for _, want := range []string{"t.four", "t.five"} {
		if ty, _ := b.nextEvent(t); ty != want {
			t.Fatalf("replayed %s, want %s", ty, want)
		}
	}
	publish(t, app, s, "t.six")
	if ty, _ := b.nextEvent(t); ty != "t.six" {
		t.Fatalf("live after the replay: %s", ty)
	}

	// an id from another run of the daemon resumes nothing
	c := openStream(t, base, app.Token, url.Values{"since": {"otherrun.2"}})
	if resumed, _ := c.hello(t); resumed {
		t.Fatal("resumed from another daemon's id")
	}
	// the Last-Event-ID header works like since
	req, _ := http.NewRequest(http.MethodGet, base+"?token="+app.Token, nil)
	req.Header.Set("Last-Event-ID", last)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	d := &sseReader{br: bufio.NewReader(resp.Body), body: resp.Body}
	defer d.body.Close()
	if resumed, _ := d.hello(t); !resumed {
		t.Fatal("Last-Event-ID did not resume")
	}
	if ty, _ := d.nextEvent(t); ty != "t.four" {
		t.Fatalf("Last-Event-ID replayed %s first", ty)
	}
}

// When what was missed has left the ring, the client is told so (it reads
// its state again) rather than handed a stream with a hole in it.
func TestStreamSaysWhenTheGapIsTooOld(t *testing.T) {
	app, s, base := streamRig(t)
	a := openStream(t, base, app.Token, nil)
	a.hello(t)
	publish(t, app, s, "t.first")
	_, first := a.nextEvent(t)
	a.body.Close()
	many := make([]string, ringEvents+5)
	for i := range many {
		many[i] = "t.flood"
	}
	publish(t, app, s, many...)
	b := openStream(t, base, app.Token, url.Values{"since": {first}})
	if resumed, _ := b.hello(t); resumed {
		t.Fatal("resumed across a gap the ring no longer covers")
	}
}

func TestStreamPingsWhileIdle(t *testing.T) {
	prev := pingEvery
	pingEvery = 50 * time.Millisecond
	defer func() { pingEvery = prev }()
	app, _, base := streamRig(t)
	a := openStream(t, base, app.Token, nil)
	a.hello(t)
	if f := a.next(t); f.event != "ping" {
		t.Fatalf("idle stream sent %+v, want a ping", f)
	}
}

// A client that stops reading is cut off once it is a buffer behind, and
// picks up from the ring when it comes back.
func TestASlowClientIsCutOffNotSkipped(t *testing.T) {
	app, s, _ := streamRig(t)
	c, _, seq, _ := s.hub.join("", "")
	n := make([]string, clientBuf+10)
	for i := range n {
		n[i] = "t.burst"
	}
	publish(t, app, s, n...)
	select {
	case <-c.gone:
	case <-time.After(5 * time.Second):
		t.Fatal("a client a whole buffer behind is still fed")
	}
	_, replay, _, resumed := s.hub.join("", s.hub.frameID(seq))
	if !resumed || len(replay) != len(n) {
		t.Fatalf("coming back: resumed=%v with %d of %d events", resumed, len(replay), len(n))
	}
}
