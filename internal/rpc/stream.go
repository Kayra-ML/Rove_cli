package rpc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/types"
)

// The event stream (/events) numbers every event and keeps the latest ones,
// so a client that lost its stream for a moment — a tunnel that dropped,
// a laptop that slept — asks for what came after the last one it saw
// (?since=, or Last-Event-ID) and is handed exactly that, instead of
// reading every screen again. A client that falls behind is not dropped
// events silently: its stream ends, and it resumes from the ring.
//
// Frames: "id: <epoch>.<seq>" then "data: <event>" for each event; a named
// "hello" first ({"epoch","seq","resumed"}: whether nothing was missed),
// and a named "ping" every pingEvery, so a client can tell a quiet stream
// from a dead one.

const (
	ringEvents = 4096    // events kept for clients coming back
	ringBytes  = 8 << 20 // and at most this much of them
	clientBuf  = 1024    // what one client may fall behind by
)

// pingEvery is how often an idle stream says it is alive (a variable so
// tests can shorten it).
var pingEvery = 15 * time.Second

type frame struct {
	seq  uint64
	ev   types.Event
	data []byte
}

type streamClient struct {
	filter string
	ch     chan frame
	// gone is closed when the client fell behind: its stream ends
	gone chan struct{}
}

type hub struct {
	mu sync.Mutex
	// epoch names this daemon's run: a seq from another run (the daemon
	// restarted) means nothing here
	epoch   string
	seq     uint64
	ring    []frame
	bytes   int
	clients map[*streamClient]struct{}
	unsub   func()
}

func newHub(bus *eventbus.Bus) *hub {
	h := &hub{
		epoch:   strconv.FormatInt(time.Now().UnixNano(), 36),
		clients: map[*streamClient]struct{}{},
	}
	if bus != nil {
		// one subscription for every stream: the bus delivers to it in
		// publish order, which is the order the seqs follow
		h.unsub = bus.Subscribe("", h.publish)
	}
	return h
}

func (h *hub) close() {
	if h.unsub != nil {
		h.unsub()
	}
}

func (h *hub) publish(ev types.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	f := frame{seq: h.seq, ev: ev, data: data}
	h.ring = append(h.ring, f)
	h.bytes += len(data)
	for len(h.ring) > ringEvents || (h.bytes > ringBytes && len(h.ring) > 1) {
		h.bytes -= len(h.ring[0].data)
		h.ring = h.ring[1:]
	}
	for c := range h.clients {
		if !eventbus.Matches(c.filter, ev) {
			continue
		}
		select {
		case c.ch <- f:
		default:
			// behind by a whole buffer: end its stream rather than skip
			// events it would never know it missed
			delete(h.clients, c)
			close(c.gone)
		}
	}
}

// join adds a client. With since (a frame id it saw last) from this run
// and still covered by the ring, the frames after it are handed back to
// send first, and resumed is true: nothing was missed. Frames after seq
// come through the client's channel; join and publish hold one lock, so
// none is missed or sent twice between the two.
func (h *hub) join(filter, since string) (c *streamClient, replay []frame, seq uint64, resumed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c = &streamClient{filter: filter, ch: make(chan frame, clientBuf), gone: make(chan struct{})}
	h.clients[c] = struct{}{}
	if epoch, n, ok := parseFrameID(since); ok && epoch == h.epoch && n <= h.seq {
		oldest := h.seq + 1
		if len(h.ring) > 0 {
			oldest = h.ring[0].seq
		}
		if n+1 >= oldest {
			resumed = true
			for _, f := range h.ring {
				if f.seq > n && eventbus.Matches(filter, f.ev) {
					replay = append(replay, f)
				}
			}
		}
	}
	return c, replay, h.seq, resumed
}

func (h *hub) leave(c *streamClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// current is the latest seq and how many streams are open.
func (h *hub) current() (seq uint64, clients int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq, len(h.clients)
}

func (h *hub) frameID(seq uint64) string { return h.epoch + "." + strconv.FormatUint(seq, 10) }

func parseFrameID(s string) (epoch string, seq uint64, ok bool) {
	epoch, n, found := strings.Cut(strings.TrimSpace(s), ".")
	if !found || epoch == "" {
		return "", 0, false
	}
	seq, err := strconv.ParseUint(n, 10, 64)
	return epoch, seq, err == nil
}

func writeFrame(w interface{ Write([]byte) (int, error) }, id string, data []byte) error {
	_, err := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", id, data)
	return err
}
