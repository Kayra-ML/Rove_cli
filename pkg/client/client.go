package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

type Client struct {
	HTTP   string
	IPC    string
	Token  string
	client *http.Client
}

func FromEnv() (*Client, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	tok := os.Getenv("ROVECODE_TOKEN")
	if tok == "" {
		tok = os.Getenv("AETHER_TOKEN")
	}
	if tok == "" {
		for _, tp := range config.TokenSearchPaths(cfg.DataDir) {
			b, err := os.ReadFile(tp)
			if err == nil && len(b) > 0 {
				tok = string(b)
				break
			}
		}
	}
	return &Client{
		HTTP:   "http://" + cfg.ListenHTTP,
		IPC:    cfg.ListenIPC,
		Token:  tok,
		client: &http.Client{Timeout: callTimeout},
	}, nil
}

// callTimeout is how long a call over HTTP waits for its answer: as long
// as the daemon may take to give one, plus the trip. Five minutes cut off
// a long chat turn on a server — its work done, its answer lost.
const callTimeout = protocol.MaxCallDuration + time.Minute

// NewHTTP is a client for a daemon reached over HTTP only — one on a
// server, through an SSH tunnel.
func NewHTTP(base, token string) *Client {
	return &Client{HTTP: base, Token: token, client: &http.Client{Timeout: callTimeout}}
}

// RemoteError is an error the daemon returned for a request it handled.
// Anything else from Call is a transport failure (daemon down, socket gone).
type RemoteError struct{ Msg string }

func (e *RemoteError) Error() string { return e.Msg }

// IsRemote reports whether err came from the daemon rather than the wire.
func IsRemote(err error) bool {
	var re *RemoteError
	return errors.As(err, &re)
}

func (c *Client) Call(method string, params any) (json.RawMessage, error) {
	raw, _ := json.Marshal(params)
	req := protocol.Request{Method: method, Params: raw, Token: c.Token}
	if c.IPC != "" {
		res, err := c.callIPC(req)
		// Fall back to HTTP only when the socket failed. A request the
		// daemon already handled must not run a second time (a sent
		// message would be sent twice).
		if err == nil || IsRemote(err) {
			return res, err
		}
	}
	return c.callHTTP(req)
}

func (c *Client) callHTTP(req protocol.Request) (json.RawMessage, error) {
	b, _ := json.Marshal(req)
	httpReq, err := http.NewRequest(http.MethodPost, c.HTTP+"/rpc", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, &RemoteError{Msg: out.Error}
	}
	return out.Result, nil
}

func (c *Client) callIPC(req protocol.Request) (json.RawMessage, error) {
	conn, err := net.DialTimeout("unix", c.IPC, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	var out protocol.Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, &RemoteError{Msg: out.Error}
	}
	return out.Result, nil
}

// Events follows the daemon's event stream until it ends, handing each
// event to handler; the stream's own frames (hello, ping) are skipped.
func (c *Client) Events(filter string, handler func(protocol.EventFrame)) error {
	url := c.HTTP + "/events?token=" + c.Token
	if filter != "" {
		url += "&filter=" + filter
	}
	// a stream lasts as long as it is followed: no timeout on it
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return ReadFrames(resp.Body, func(f Frame) {
		if f.Name != "" {
			return
		}
		var ev struct {
			Type    string          `json:"type"`
			Topic   string          `json:"topic"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal([]byte(f.Data), &ev) != nil {
			return
		}
		handler(protocol.EventFrame{Type: ev.Type, Topic: ev.Topic, Payload: ev.Payload})
	})
}

// Frame is one frame of the daemon's event stream: an event (Name empty),
// with the id a client resumes after (?since=), or one of the stream's own
// ("hello" first, then "ping" while idle).
type Frame struct {
	Name string
	ID   string
	Data string
}

// Hello is a stream's first frame: where it starts, and whether it resumed
// after the id it was asked to — nothing was missed.
type Hello struct {
	Epoch   string `json:"epoch"`
	Seq     uint64 `json:"seq"`
	Resumed bool   `json:"resumed"`
}

// ReadFrames reads an event stream frame by frame until it ends (nil at
// its end, the read error otherwise). Comments are skipped.
func ReadFrames(r io.Reader, fn func(Frame)) error {
	br := bufio.NewReader(r)
	var f Frame
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if f.Data != "" || f.Name != "" {
				fn(f)
			}
			f = Frame{}
		case strings.HasPrefix(line, ":"):
		default:
			k, v, _ := strings.Cut(line, ":")
			v = strings.TrimPrefix(v, " ")
			switch k {
			case "event":
				f.Name = v
			case "id":
				f.ID = v
			case "data":
				if f.Data != "" {
					f.Data += "\n"
				}
				f.Data += v
			}
		}
	}
}
