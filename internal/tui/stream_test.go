package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A stream that comes back asks for what came after its last event; only
// when that cannot be had is the screen read again.
func TestTheEventStreamResumesOrSaysThereWasAGap(t *testing.T) {
	var reply string
	var since []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		since = append(since, r.URL.Query().Get("since"))
		fmt.Fprint(w, reply)
	}))
	defer srv.Close()
	c := &IPCClient{httpBase: srv.URL, token: "t"}
	follow := func(last *string, again bool) []tea.Msg {
		var got []tea.Msg
		if _, err := c.followEvents(last, again, func(m tea.Msg) { got = append(got, m) }); err != nil {
			t.Fatal(err)
		}
		return got
	}
	gaps := func(msgs []tea.Msg) (n int) {
		for _, m := range msgs {
			if _, ok := m.(StreamGapMsg); ok {
				n++
			}
		}
		return n
	}
	last := ""
	reply = "event: hello\ndata: {\"resumed\":false}\n\nid: e.1\ndata: {\"type\":\"a\"}\n\n"
	msgs := follow(&last, false)
	if last != "e.1" || gaps(msgs) != 0 {
		t.Fatalf("first stream: last=%q msgs=%+v", last, msgs)
	}
	if ev, ok := msgs[len(msgs)-1].(EventMsg); !ok || ev.Frame.Type != "a" {
		t.Fatalf("event = %+v", msgs[len(msgs)-1])
	}

	reply = "event: hello\ndata: {\"resumed\":true}\n\nid: e.2\ndata: {\"type\":\"b\"}\n\n"
	if msgs := follow(&last, true); gaps(msgs) != 0 || last != "e.2" || since[1] != "e.1" {
		t.Fatalf("resumed: since=%v gaps=%d last=%q", since, gaps(msgs), last)
	}
	reply = "event: hello\ndata: {\"resumed\":false}\n\n"
	if gaps(follow(&last, true)) != 1 {
		t.Fatal("a stream that could not resume said nothing")
	}
	// an older daemon sends no hello: the gap cannot have been covered
	reply = "data: {\"type\":\"c\"}\n\n"
	if gaps(follow(&last, true)) != 1 {
		t.Fatal("no hello was taken for a resume")
	}
}
