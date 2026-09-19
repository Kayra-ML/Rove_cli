package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/pkg/protocol"
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
		b, err := os.ReadFile(config.TokenPath(cfg.DataDir))
		if err == nil {
			tok = string(b)
		}
	}
	return &Client{
		HTTP:   "http://" + cfg.ListenHTTP,
		IPC:    cfg.ListenIPC,
		Token:  tok,
		client: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (c *Client) Call(method string, params any) (json.RawMessage, error) {
	raw, _ := json.Marshal(params)
	req := protocol.Request{Method: method, Params: raw, Token: c.Token}
	if c.IPC != "" {
		if res, err := c.callIPC(req); err == nil {
			return res, nil
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
		return nil, fmt.Errorf("%s", out.Error)
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
		return nil, fmt.Errorf("%s", out.Error)
	}
	return out.Result, nil
}

func (c *Client) Events(filter string, handler func(protocol.EventFrame)) error {
	url := c.HTTP + "/events?token=" + c.Token
	if filter != "" {
		url += "&filter=" + filter
	}
	resp, err := c.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = trimData(line)
		if line == "" {
			continue
		}
		var ev struct {
			Type    string          `json:"type"`
			Topic   string          `json:"topic"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		handler(protocol.EventFrame{Type: ev.Type, Topic: ev.Topic, Payload: ev.Payload})
	}
}

func trimData(s string) string {
	s = stringBytesTrim(s)
	if len(s) >= 5 && s[:5] == "data:" {
		return stringBytesTrim(s[5:])
	}
	return ""
}

func stringBytesTrim(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\r' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\r' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}
