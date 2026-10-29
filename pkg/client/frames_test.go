package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestReadFramesSplitsTheStream(t *testing.T) {
	stream := ": connected\n\n" +
		"event: hello\ndata: {\"resumed\":true}\n\n" +
		"id: e.1\ndata: {\"type\":\"a\"}\n\n" +
		"id: e.2\r\ndata: line one\r\ndata: line two\r\n\r\n" +
		"event: ping\ndata: {}\n\n"
	var got []Frame
	if err := ReadFrames(strings.NewReader(stream), func(f Frame) { got = append(got, f) }); err != nil {
		t.Fatal(err)
	}
	want := []Frame{
		{Name: "hello", Data: `{"resumed":true}`},
		{ID: "e.1", Data: `{"type":"a"}`},
		{ID: "e.2", Data: "line one\nline two"},
		{Name: "ping", Data: "{}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frames = %+v", got)
	}
}

// Events hands over events only, never the stream's own frames.
func TestEventsSkipsTheStreamsOwnFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, ": connected\n\nevent: hello\ndata: {\"seq\":0}\n\nid: e.1\ndata: {\"type\":\"goal.updated\",\"topic\":\"goal.g\"}\n\nevent: ping\ndata: {\"seq\":1}\n\n")
	}))
	defer srv.Close()
	c := NewHTTP(srv.URL, "tok")
	var got []protocol.EventFrame
	if err := c.Events("", func(f protocol.EventFrame) { got = append(got, f) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "goal.updated" || got[0].Topic != "goal.g" {
		t.Fatalf("events = %+v", got)
	}
}
