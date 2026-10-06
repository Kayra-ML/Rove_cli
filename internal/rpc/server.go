package rpc

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kayra-ML/rove/internal/sshtunnel"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/approval"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/marketplace"
	"github.com/Kayra-ML/rove/internal/persona"
	"github.com/Kayra-ML/rove/internal/terminal"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/internal/usage"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

type Server struct {
	app  *core.App
	mu   sync.Mutex
	ln   net.Listener
	http *http.Server
}

func New(app *core.App) *Server { return &Server{app: app} }

// maxBodyBytes caps incoming request bodies to 4 MiB to prevent OOM DoS.
const maxBodyBytes = 4 << 20 // 4 MiB

// ServeHTTPOn serves on a listener the caller already bound, so a port
// conflict surfaces before the daemon reports itself ready.
func (s *Server) ServeHTTPOn(ln net.Listener) error {
	addr := ln.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.app.Health())
	})
	mux.HandleFunc("/rpc", s.handleHTTPRPC)
	mux.HandleFunc("/webhook/", s.handleWebhookTrigger)
	mux.HandleFunc("/events", s.handleSSE)
	s.http = &http.Server{
		Addr:    addr,
		Handler: withCORS(mux),
		// Reasonable timeouts — prevents slow-loris and hung connections.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       60 * time.Second,
	}
	return s.http.Serve(ln)
}

func (s *Server) ServeIPC(path string) error {
	_ = os.Remove(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	for {
		c, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return nil // Close
		}
		if err != nil {
			return err
		}
		go s.serveConn(c)
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.ln != nil {
		err = s.ln.Close()
	}
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = s.http.Shutdown(ctx)
	}
	return err
}

func (s *Server) serveConn(c net.Conn) {
	defer c.Close()
	dec := json.NewDecoder(bufio.NewReader(c))
	enc := json.NewEncoder(c)
	for {
		var req protocol.Request
		if err := dec.Decode(&req); err != nil {
			if err != io.EOF {
				_ = enc.Encode(protocol.Response{OK: false, Error: err.Error()})
			}
			return
		}
		resp := s.Dispatch(context.Background(), req)
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

// rpcWriteLimit bounds how long one call may take to answer over HTTP;
// httpWriteTimeout is the server's default for everything else.
const rpcWriteLimit = time.Hour

var httpWriteTimeout = 120 * time.Second

func (s *Server) handleHTTPRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	// A call answers when its work is done: a chat turn, a plan or a check
	// can take minutes. The server-wide WriteTimeout (two minutes) would
	// close the connection on the answer of a longer one — the work done,
	// its result lost, the app shown an empty reply. The request body was
	// read under ReadTimeout; the answer gets the time the work takes.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(rpcWriteLimit))
	var req protocol.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, protocol.Response{OK: false, Error: err.Error()})
		return
	}
	if req.Token == "" {
		req.Token = bearer(r)
	}
	resp := s.Dispatch(r.Context(), req)
	writeJSON(w, 200, resp)
}

// handleWebhookTrigger handles POST /webhook/<eventType> requests from external
// systems (e.g. GitHub). It verifies the HMAC-SHA256 signature when a secret
// is configured and fires matching webhook rules.
func (s *Server) handleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	// Extract event type from URL path: /webhook/<eventType>
	eventType := strings.TrimPrefix(r.URL.Path, "/webhook/")
	if eventType == "" {
		eventType = r.Header.Get("X-GitHub-Event")
	}
	if eventType == "" {
		eventType = "*"
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if s.app.Webhook == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "webhook engine not initialised"})
		return
	}
	results, err := s.app.Webhook.Process(r.Context(), eventType, body, sig)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = bearer(r)
	}
	if !tokenOK(tok, s.app.Token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	// An event stream lives as long as the app is open; the server-wide
	// WriteTimeout would cut it every two minutes and drop whatever fired
	// while the client reconnects.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Flush headers immediately. Without an initial frame, clients block inside
	// http.Get until the first real event and falsely remain "offline".
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	filter := r.URL.Query().Get("filter")
	// The desktop app multiplexes all its subscriptions over one unfiltered
	// stream, so a burst (a streamed reply, terminal output) must fit here
	// or events are dropped.
	ch := make(chan types.Event, 1024)
	unsub := s.app.Bus.Subscribe(filter, func(ev types.Event) {
		select {
		case ch <- ev:
		default:
		}
	})
	defer unsub()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			payload, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

// tokenOK compares in constant time, so the answer's timing says nothing
// of the token; an app without a token lets nothing in.
func tokenOK(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	return strings.TrimPrefix(h, "Bearer ")
}

// withCORS adds CORS headers. The HTTP server only listens on 127.0.0.1 so
// in production only the Wails webview (which uses a wails:// or localhost
// origin) can reach it. We still restrict the allowed origin explicitly so
// that an arbitrary browser tab on the same machine cannot make credentialed
// cross-origin requests.
func withCORS(h http.Handler) http.Handler {
	// Allowed origins: Wails desktop webview and local dev server.
	allowed := map[string]struct{}{
		"wails://wails":          {},
		"http://localhost":       {},
		"http://localhost:7420":  {},
		"http://localhost:34115": {},
		"http://127.0.0.1":       {},
		"http://127.0.0.1:7420":  {},
		"http://127.0.0.1:34115": {},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// No Origin header → direct/native call, no CORS needed.
			h.ServeHTTP(w, r)
			return
		}
		if _, ok := allowed[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) Dispatch(ctx context.Context, req protocol.Request) protocol.Response {
	if req.ID == "" {
		req.ID = string(id.NewID())
	}
	if req.Method != protocol.MethodPing && !tokenOK(req.Token, s.app.Token) {
		return protocol.Response{ID: req.ID, OK: false, Error: "unauthorized"}
	}
	res, err := s.handle(ctx, req)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("rpc %s: %v", req.Method, err)
		}
		return protocol.Response{ID: req.ID, OK: false, Error: err.Error()}
	}
	return protocol.Response{ID: req.ID, OK: true, Result: res}
}

func (s *Server) handle(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	a := s.app
	switch req.Method {
	case protocol.MethodPing:
		return core.MustJSON(a.Health()), nil
	case protocol.MethodUsageGet:
		return core.MustJSON(a.Health()), nil
	case protocol.MethodUsageReport:
		var p struct {
			Days int `json:"days"`
		}
		if len(req.Params) > 0 && string(req.Params) != "null" {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, err
			}
		}
		if p.Days <= 0 {
			p.Days = 30
		}
		if p.Days > 366 {
			p.Days = 366
		}
		now := time.Now()
		entries, err := a.Store.ListUsageSince(ctx, usage.Start(p.Days, now, time.Local))
		if err != nil {
			return nil, err
		}
		return core.MustJSON(usage.Build(entries, p.Days, now, time.Local)), nil
	case protocol.MethodAgentList:
		list, err := a.Agents.List(ctx)
		return core.MustJSON(list), err
	case protocol.MethodAgentUpsert:
		var agentRow types.Agent
		if err := json.Unmarshal(req.Params, &agentRow); err != nil {
			return nil, err
		}
		out, err := a.Agents.Upsert(ctx, agentRow)
		return core.MustJSON(out), err
	case protocol.MethodSessionCreate:
		var p struct {
			Title       string     `json:"title"`
			AgentID     types.ID   `json:"agentId"`
			WorkspaceID types.ID   `json:"workspaceId"`
			Workspace   string     `json:"workspace"`
			Cwd         string     `json:"cwd"`
			ProfileIDs  []types.ID `json:"profileIds"`
			// CharacterID starts the chat with that catalog character (an
			// agent picked in Office).
			CharacterID string `json:"characterId"`
			// Space is types.SpaceOffice or types.SpaceChat.
			Space string `json:"space"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.AgentID == "" {
			if agents, err := a.Agents.List(ctx); err == nil && len(agents) > 0 {
				p.AgentID = agents[0].ID
			}
		}
		if p.WorkspaceID == "" {
			path := strings.TrimSpace(p.Workspace)
			if path == "" {
				path = strings.TrimSpace(p.Cwd)
			}
			if path == "" {
				path = fallbackDir()
			}
			if path != "" {
				name := filepath.Base(path)
				ws, err := a.WS.Open(ctx, path, name)
				if err != nil {
					return nil, err
				}
				p.WorkspaceID = ws.ID
			}
		}
		if p.CharacterID != "" {
			if _, ok := persona.CharacterByID(p.CharacterID); !ok {
				return nil, fmt.Errorf("unknown character %q", p.CharacterID)
			}
		}
		switch p.Space {
		case "", types.SpaceOffice, types.SpaceChat:
		default:
			return nil, fmt.Errorf("unknown space %q", p.Space)
		}
		out, err := a.Sess.CreateIn(ctx, p.Space, p.Title, p.AgentID, p.WorkspaceID)
		if err == nil && p.CharacterID != "" {
			err = a.Store.PutSessionPersona(ctx, types.SessionPersona{SessionID: out.ID, CharacterID: p.CharacterID, UpdatedAt: time.Now().UTC()})
		}
		if err == nil && len(p.ProfileIDs) > 0 {
			// a new chat started with an Office agent picked: it answers there
			if perr := setChatAgent(ctx, a, out.ID, p.ProfileIDs); perr != nil {
				_ = a.Sess.Delete(ctx, out.ID)
				return nil, perr
			}
		}
		return core.MustJSON(out), err
	case protocol.MethodSessionList:
		var p struct {
			WorkspaceID types.ID `json:"workspaceId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		out, err := a.Sess.List(ctx, p.WorkspaceID)
		return core.MustJSON(out), err
	case protocol.MethodSessionHistory:
		var p struct {
			SessionID types.ID `json:"sessionId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Sess.History(ctx, p.SessionID)
		return core.MustJSON(out), err
	case protocol.MethodSessionSend:
		var p struct {
			SessionID   types.ID `json:"sessionId"`
			AgentID     types.ID `json:"agentId"`
			WorkspaceID types.ID `json:"workspaceId"`
			Workspace   string   `json:"workspace"`
			Content     string   `json:"content"`
			Images      []string `json:"images"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if err := checkImages(p.Images); err != nil {
			return nil, err
		}
		// Session identity is authoritative. Terminal and desktop clients only
		// need to send a session ID; infer the bound agent/workspace here.
		if p.AgentID == "" || p.WorkspaceID == "" {
			sess, err := a.Sess.Get(ctx, p.SessionID)
			if err != nil {
				return nil, err
			}
			if p.AgentID == "" {
				p.AgentID = sess.AgentID
			}
			if p.WorkspaceID == "" {
				p.WorkspaceID = sess.WorkspaceID
			}
		}
		if p.Workspace == "" && p.WorkspaceID != "" {
			if ws, err := a.WS.Get(ctx, p.WorkspaceID); err == nil {
				p.Workspace = ws.Path
			}
		}
		if p.Workspace == "" {
			if cwd := fallbackDir(); cwd != "" {
				if ws, err := a.WS.Open(ctx, cwd, filepath.Base(cwd)); err == nil {
					p.WorkspaceID = ws.ID
					p.Workspace = ws.Path
				}
			}
		}
		// Name a fresh chat after its first message before the run starts, so
		// the session list shows the topic right away, not when the reply ends.
		if p.SessionID != "" {
			if sess, gerr := a.Sess.Get(ctx, p.SessionID); gerr == nil && sess.ParentID == "" && placeholderTitle(sess.Title) {
				if title := sessionTitleFrom(p.Content); title != "" {
					_ = a.Sess.Rename(ctx, p.SessionID, title)
				}
			} else if gerr == nil && sess.Space == types.SpaceTerminal {
				// a session opened in a second terminal is named by that
				// terminal's first message
				if parent, perr := a.Sess.Get(ctx, sess.ParentID); perr == nil && placeholderTitle(parent.Title) {
					if title := sessionTitleFrom(p.Content); title != "" {
						_ = a.Sess.Rename(ctx, parent.ID, title)
					}
				}
			}
		}
		res, err := a.Agents.Run(ctx, agent.RunRequest{
			AgentID:     p.AgentID,
			SessionID:   p.SessionID,
			WorkspaceID: p.WorkspaceID,
			Workspace:   p.Workspace,
			UserMessage: p.Content,
			Images:      p.Images,
		})
		return core.MustJSON(res), err
	case protocol.MethodSessionRename:
		var p struct {
			ID    types.ID `json:"id"`
			Title string   `json:"title"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Sess.Rename(ctx, p.ID, p.Title)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodSessionDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Sess.Delete(ctx, p.ID)
		if err == nil {
			_ = a.Store.DeleteMapNode(ctx, p.ID)
		}
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodSessionTruncate:
		var p struct {
			SessionID types.ID `json:"sessionId"`
			Keep      int      `json:"keep"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Keep < 0 {
			p.Keep = 0
		}
		err := a.Sess.Truncate(ctx, p.SessionID, p.Keep)
		// a connected agent system's session still holds what was cut
		a.AgentSessions.Forget(string(p.SessionID))
		if err != nil {
			return nil, err
		}
		hist, herr := a.Sess.History(ctx, p.SessionID)
		if herr != nil {
			return nil, herr
		}
		return core.MustJSON(hist), nil
	case protocol.MethodSessionAppend:
		var p struct {
			SessionID types.ID        `json:"sessionId"`
			Messages  []types.Message `json:"messages"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		for i := range p.Messages {
			p.Messages[i].SessionID = p.SessionID
			if _, err := a.Sess.Append(ctx, p.Messages[i]); err != nil {
				return nil, err
			}
		}
		hist, herr := a.Sess.History(ctx, p.SessionID)
		if herr != nil {
			return nil, herr
		}
		return core.MustJSON(hist), nil
	case protocol.MethodGoalCreate:
		var g types.Goal
		if err := json.Unmarshal(req.Params, &g); err != nil {
			return nil, err
		}
		out, err := a.Goals.Create(ctx, g)
		return core.MustJSON(out), err
	case protocol.MethodGoalDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Goals.Delete(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodGoalList:
		out, err := a.Goals.List(ctx)
		return core.MustJSON(out), err
	case protocol.MethodGoalGet:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Goals.Get(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodGoalDrive:
		var p struct {
			ID        types.ID `json:"id"`
			SessionID types.ID `json:"sessionId"`
			Workspace string   `json:"workspace"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		// runs as long as the daemon, until done or canceled
		if err := a.Goals.Start(p.ID, p.SessionID, p.Workspace); err != nil {
			return nil, err
		}
		return core.MustJSON(map[string]any{"started": true}), nil
	case protocol.MethodWorkspaceOpen:
		var p struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.WS.Open(ctx, p.Path, p.Name)
		return core.MustJSON(out), err
	case protocol.MethodWorkspaceList:
		out, err := a.WS.List(ctx)
		return core.MustJSON(out), err
	case protocol.MethodTerminalSpawn:
		var p terminal.SpawnOpts
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Term.Spawn(ctx, p)
		return core.MustJSON(out), err
	case protocol.MethodTerminalList:
		return core.MustJSON(a.Term.List()), nil
	case protocol.MethodTerminalWrite:
		var p struct {
			ID   types.ID `json:"id"`
			Data string   `json:"data"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Term.Write(p.ID, []byte(p.Data))
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodTerminalResize:
		var p struct {
			ID   types.ID `json:"id"`
			Cols int      `json:"cols"`
			Rows int      `json:"rows"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Term.Resize(p.ID, p.Cols, p.Rows)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodTerminalRestart:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Term.Restart(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodTerminalAttach:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		meta, replay, err := a.Term.Attach(p.ID)
		return core.MustJSON(map[string]any{"session": meta, "replay": string(replay)}), err
	case protocol.MethodTerminalDetach:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Term.Detach(p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodFileDirs:
		return s.handleDirs(ctx, req)
	case protocol.MethodTerminalPanes, protocol.MethodTerminalNewPane, protocol.MethodTerminalSendPane,
		protocol.MethodTerminalLayoutGet, protocol.MethodTerminalLayoutSet:
		return s.handlePanes(ctx, req)
	case protocol.MethodSSHDiscover:
		return core.MustJSON(sshtunnel.Discover("")), nil
	case protocol.MethodSSHOpen:
		var p types.SSHTarget
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.SSH.Open(ctx, p, "", "")
		return core.MustJSON(out), err
	case protocol.MethodSkillList:
		out, err := a.Skills.List()
		return core.MustJSON(out), err
	case protocol.MethodSkillInstall:
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Market.Install(p.Name)
		return core.MustJSON(out), err
	case protocol.MethodSkillUninstall:
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Skills.Uninstall(p.Name)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodSkillSetEnabled:
		var p struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Skills.SetEnabled(p.Name, p.Enabled)
		return core.MustJSON(out), err
	case protocol.MethodWorkspaceDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.WS.Delete(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodProviderDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.DeleteProvider(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodMarketList:
		var p struct {
			Q string `json:"q"`
		}
		_ = json.Unmarshal(req.Params, &p)
		out, err := a.Market.Search(p.Q)
		return core.MustJSON(out), err
	case protocol.MethodProviderList:
		out, err := a.Store.ListProviders(ctx)
		return core.MustJSON(out), err
	case protocol.MethodProviderUpsert:
		var p types.Provider
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.SaveProvider(ctx, p)
		return core.MustJSON(out), err
	case protocol.MethodProviderRefresh:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.RefreshProviderModels(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodSecretPut:
		var p struct {
			ID    string `json:"id"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Secrets.Put(p.ID, p.Value)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodPermissionList:
		out, err := a.Store.ListPermissions(ctx)
		return core.MustJSON(out), err
	case protocol.MethodPermissionPut:
		var r types.PermissionRule
		if err := json.Unmarshal(req.Params, &r); err != nil {
			return nil, err
		}
		err := a.Perm.Put(ctx, r)
		return core.MustJSON(r), err
	case protocol.MethodPermissionAsks:
		if a.Approvals == nil {
			return core.MustJSON([]approval.Request{}), nil
		}
		return core.MustJSON(a.Approvals.Pending()), nil
	case protocol.MethodPermissionAnswer:
		if a.Approvals == nil {
			return nil, fmt.Errorf("approvals not available")
		}
		var p struct {
			ID       types.ID `json:"id"`
			Allow    bool     `json:"allow"`
			Remember bool     `json:"remember"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Approvals.Answer(p.ID, approval.Answer{Allow: p.Allow, Remember: p.Remember})
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodMemoryPut:
		var p struct {
			Scope   types.MemoryScope `json:"scope"`
			ScopeID types.ID          `json:"scopeId"`
			Key     string            `json:"key"`
			Content string            `json:"content"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Mem.Remember(ctx, p.Scope, p.ScopeID, p.Key, p.Content)
		return core.MustJSON(out), err
	case protocol.MethodMemoryList:
		var p struct {
			Scope   types.MemoryScope `json:"scope"`
			ScopeID types.ID          `json:"scopeId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Mem.List(ctx, p.Scope, p.ScopeID)
		return core.MustJSON(out), err
	case protocol.MethodFileTree:
		var p struct {
			Path        string   `json:"path"`
			WorkspaceID types.ID `json:"workspaceId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		// Restrict tree walk to a registered workspace root so arbitrary
		// directory enumeration is not possible via this RPC.
		root := p.Path
		if p.WorkspaceID != "" {
			ws, werr := a.WS.Get(ctx, p.WorkspaceID)
			if werr != nil {
				return nil, fmt.Errorf("workspace not found: %w", werr)
			}
			root = ws.Path
		} else if root == "" {
			return nil, fmt.Errorf("path or workspaceId required")
		}
		out, err := a.FileTree(root, 800)
		return core.MustJSON(out), err
	case protocol.MethodFileRead:
		var p struct {
			Path        string   `json:"path"`
			WorkspaceID types.ID `json:"workspaceId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		// Resolve path relative to workspace when a workspaceId is given.
		// When no workspace is set (e.g. direct CLI use) we still validate
		// that the path is absolute and exists, but we cannot bound it further.
		resolved := p.Path
		if p.WorkspaceID != "" {
			ws, werr := a.WS.Get(ctx, p.WorkspaceID)
			if werr != nil {
				return nil, fmt.Errorf("workspace not found: %w", werr)
			}
			clean := filepath.Clean(p.Path)
			if !filepath.IsAbs(clean) {
				clean = filepath.Join(ws.Path, clean)
			}
			abs, aerr := filepath.Abs(clean)
			if aerr != nil {
				return nil, aerr
			}
			wsAbs, _ := filepath.Abs(ws.Path)
			rel, rerr := filepath.Rel(wsAbs, abs)
			if rerr != nil || strings.HasPrefix(rel, "..") {
				return nil, fmt.Errorf("path escapes workspace")
			}
			resolved = abs
		}
		b, err := os.ReadFile(resolved)
		if err != nil {
			return nil, err
		}
		if len(b) > 256*1024 {
			b = b[:256*1024]
		}
		return core.MustJSON(map[string]any{"content": string(b)}), nil
	case protocol.MethodShutdown:
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
		return core.MustJSON(map[string]any{"ok": true}), nil

	// ── Harness policy ────────────────────────────────────────────────────────

	case protocol.MethodAgentDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.DeleteAgent(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodAutomationList:
		if a.Auto == nil {
			return core.MustJSON([]types.AutomationJob{}), nil
		}
		out, err := a.Auto.List(ctx)
		return core.MustJSON(out), err
	case protocol.MethodAutomationUpsert:
		if a.Auto == nil {
			return nil, fmt.Errorf("automation off")
		}
		var j types.AutomationJob
		if err := json.Unmarshal(req.Params, &j); err != nil {
			return nil, err
		}
		out, err := a.Auto.Upsert(ctx, j)
		return core.MustJSON(out), err
	case protocol.MethodAutomationDelete:
		if a.Auto == nil {
			return nil, fmt.Errorf("automation off")
		}
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Auto.Delete(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodAutomationTick:
		if a.Auto == nil {
			return core.MustJSON([]types.AutomationJob{}), nil
		}
		out, err := a.Auto.Tick(ctx)
		return core.MustJSON(out), err

	// ── Checkpoint / snapshot ──────────────────────────────────────────────
	case protocol.MethodCheckpointTake:
		var p struct {
			Path  string `json:"path"`
			Label string `json:"label"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		snap, err := a.Checkpt.Take(p.Path, p.Label)
		return core.MustJSON(snap), err
	case protocol.MethodCheckpointList:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		snaps, err := a.Checkpt.List(p.Path)
		return core.MustJSON(snaps), err
	case protocol.MethodCheckpointRestore:
		var p struct {
			Path string `json:"path"`
			Ref  string `json:"ref"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Checkpt.Restore(p.Path, p.Ref)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	// ── Diff hunk accept/reject ────────────────────────────────────────────

	// ── Session export / import ────────────────────────────────────────────
	case protocol.MethodSessionExport:
		var p struct {
			SessionID types.ID `json:"sessionId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		msgs, err := a.Sess.History(ctx, p.SessionID)
		if err != nil {
			return nil, err
		}
		var sb strings.Builder
		sb.WriteString("# Session Export\n\n")
		for _, m := range msgs {
			sb.WriteString("## ")
			sb.WriteString(string(m.Role))
			sb.WriteString("\n\n")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n---\n\n")
		}
		filename := "session-" + string(p.SessionID) + ".md"
		return core.MustJSON(map[string]any{"markdown": sb.String(), "filename": filename}), nil

	// ── Git branch / push / PR ─────────────────────────────────────────────

	// ── MCP server registry ────────────────────────────────────────────────
	case protocol.MethodMCPList:
		out, err := a.Store.ListMCPServers(ctx)
		if err != nil {
			return nil, err
		}
		return core.MustJSON(out), nil

	case protocol.MethodMCPAdd:
		var srv types.MCPServerConfig
		if err := json.Unmarshal(req.Params, &srv); err != nil {
			return nil, err
		}
		// Basic validation: Command must be a non-empty, non-shell-injection string.
		// It must not contain shell metacharacters since it is passed directly to
		// exec.CommandContext (not a shell), but a future code path might change
		// that, so we reject obviously dangerous values early.
		if srv.Command == "" {
			return nil, fmt.Errorf("mcp: command is required")
		}
		for _, bad := range []string{";", "&&", "||", "|", "`", "$(", "${", "\n", "\r"} {
			if strings.Contains(srv.Command, bad) {
				return nil, fmt.Errorf("mcp: command contains disallowed characters")
			}
		}
		if srv.ID == "" {
			srv.ID = string(id.NewID())
		}
		// a renamed server stops under its old name
		if list, err := a.Store.ListMCPServers(ctx); err == nil && a.MCP != nil {
			for _, prev := range list {
				if prev.ID == srv.ID && prev.Name != srv.Name {
					_ = a.MCP.Stop(prev.Name)
				}
			}
		}
		if err := a.Store.UpsertMCPServer(ctx, srv); err != nil {
			return nil, err
		}
		go func() { _ = a.StartMCP(context.Background(), srv) }()
		return core.MustJSON(srv), nil

	case protocol.MethodMCPRemove:
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if list, err := a.Store.ListMCPServers(ctx); err == nil && a.MCP != nil {
			for _, srv := range list {
				if srv.ID == p.ID {
					_ = a.MCP.Stop(srv.Name)
				}
			}
		}
		err := a.Store.DeleteMCPServer(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodMCPDiscover:
		if a.MCP == nil {
			return core.MustJSON([]string{}), nil
		}
		return core.MustJSON(a.MCP.List()), nil

	// ── Webhook trigger rules ─────────────────────────────────────────────
	case protocol.MethodWebhookList:
		if a.Webhook == nil {
			return core.MustJSON([]types.WebhookRule{}), nil
		}
		out, err := a.Webhook.List(ctx)
		return core.MustJSON(out), err
	case protocol.MethodWebhookUpsert:
		if a.Webhook == nil {
			return nil, fmt.Errorf("webhook engine not available")
		}
		var r types.WebhookRule
		if err := json.Unmarshal(req.Params, &r); err != nil {
			return nil, err
		}
		out, err := a.Webhook.Upsert(ctx, r)
		return core.MustJSON(out), err
	case protocol.MethodWebhookDelete:
		if a.Webhook == nil {
			return nil, fmt.Errorf("webhook engine not available")
		}
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Webhook.Delete(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	// ── Codebase FTS5 index ───────────────────────────────────────────────
	case protocol.MethodIndexBuild:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if a.Index == nil {
			return nil, fmt.Errorf("index not initialised")
		}
		go func() {
			_ = a.Index.IndexWorkspace(context.Background(), p.Path)
		}()
		return core.MustJSON(map[string]any{"started": true}), nil

	case protocol.MethodIndexSearch:
		var p struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if a.Index == nil {
			return nil, fmt.Errorf("index not initialised")
		}
		if p.Limit <= 0 {
			p.Limit = 10
		}
		results, err := a.Index.Search(ctx, p.Query, p.Limit)
		return core.MustJSON(results), err

	// ── Automation catalog / install / uninstall ─────────────────────────
	case protocol.MethodAutomationCatalog:
		jobs, _ := a.Auto.List(ctx)
		installed := map[string]bool{}
		for _, j := range jobs {
			installed[strings.ToLower(j.Name)] = true
		}
		templates, err := marketplace.ListAutomationTemplates(installed)
		return core.MustJSON(templates), err

	case protocol.MethodAutomationInstall:
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		// Find template in catalog
		jobs, _ := a.Auto.List(ctx)
		installed := map[string]bool{}
		for _, j := range jobs {
			installed[strings.ToLower(j.Name)] = true
		}
		templates, err := marketplace.ListAutomationTemplates(installed)
		if err != nil {
			return nil, err
		}
		var tmpl *types.AutomationTemplate
		for i := range templates {
			if strings.EqualFold(templates[i].Name, p.Name) {
				tmpl = &templates[i]
				break
			}
		}
		if tmpl == nil {
			return nil, fmt.Errorf("automation template %q not found", p.Name)
		}
		if tmpl.Installed {
			return nil, fmt.Errorf("automation %q is already installed", p.Name)
		}
		kind := types.AutomationKind(strings.ReplaceAll(strings.ToLower(tmpl.Name), "-", "_"))
		job := types.AutomationJob{
			ID:           id.NewID(),
			Name:         tmpl.Name,
			Kind:         kind,
			EverySeconds: tmpl.EverySeconds,
			Enabled:      true,
		}
		out, err := a.Auto.Upsert(ctx, job)
		return core.MustJSON(out), err

	case protocol.MethodAutomationUninstall:
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		jobs, err := a.Auto.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			if strings.EqualFold(j.Name, p.Name) {
				if delErr := a.Auto.Delete(ctx, j.ID); delErr != nil {
					return nil, delErr
				}
				return core.MustJSON(map[string]any{"ok": true}), nil
			}
		}
		return nil, fmt.Errorf("automation %q not installed", p.Name)

	// ── Agent profiles ─────────────────────────────────────────────────────
	case protocol.MethodProfileList:
		out, err := a.Store.ListAgentProfiles(ctx)
		return core.MustJSON(out), err

	case protocol.MethodProfileUpsert:
		var p types.AgentProfile
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.ID == "" {
			p.ID = id.NewID()
		}
		// an office agent is checked like a picked model or character: a
		// typo here would only show up as a failed chat later
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			return nil, fmt.Errorf("an agent needs a name")
		}
		if p.Model != "" && p.Model != "default" {
			if err := s.knownModel(ctx, p.Provider, p.Model); err != nil {
				return nil, err
			}
		}
		if p.CharacterID != "" {
			if _, ok := persona.CharacterByID(p.CharacterID); !ok {
				return nil, fmt.Errorf("unknown character %q", p.CharacterID)
			}
		}
		if p.PromptMode != "" && p.PromptMode != types.PromptOwn {
			return nil, fmt.Errorf("unknown prompt mode %q", p.PromptMode)
		}
		if p.Role == "" {
			p.Role = types.RoleDeveloper
		}
		out, err := a.Store.UpsertAgentProfile(ctx, p)
		if err == nil && p.IsDefault {
			// only one default profile
			err = a.Store.SetDefaultProfile(ctx, p.ID)
		}
		return core.MustJSON(out), err

	case protocol.MethodProfileDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.DeleteAgentProfile(ctx, p.ID)
		if err == nil {
			// a removed agent's tasks and watches go with it
			_ = a.Store.DeleteStaffTasks(ctx, p.ID)
			_ = a.Store.DeleteStaffWatchesOf(ctx, p.ID)
			_ = a.Store.DeleteStaffSchedulesOf(ctx, p.ID)
			_ = a.Store.DeleteStaffMonitorsOf(ctx, p.ID)
			_ = a.Store.DeleteStaffHandoffsOf(ctx, p.ID)
		}
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodProfileSetDefault:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.SetDefaultProfile(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	// ── Session linking & relay ─────────────────────────────────────────────
	case protocol.MethodSessionLink:
		var p struct {
			SessionA types.ID `json:"sessionA"`
			SessionB types.ID `json:"sessionB"`
			Label    string   `json:"label"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		link := types.SessionLink{SessionA: p.SessionA, SessionB: p.SessionB, Label: p.Label}
		out, err := a.Store.CreateSessionLink(ctx, link)
		return core.MustJSON(out), err

	case protocol.MethodSessionUnlink:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.DeleteSessionLink(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodSessionLinked:
		var p struct {
			SessionID types.ID `json:"sessionId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Store.ListSessionLinks(ctx, p.SessionID)
		return core.MustJSON(out), err

	case protocol.MethodSessionRelay:
		var p struct {
			FromSessionID types.ID `json:"fromSessionId"`
			ToSessionID   types.ID `json:"toSessionId"`
			Content       string   `json:"content"`
			FromRole      string   `json:"fromRole"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		toSess, err := a.Store.GetSession(ctx, p.ToSessionID)
		if err != nil {
			return nil, fmt.Errorf("target session not found: %w", err)
		}
		prefix := "[relay"
		if p.FromRole != "" {
			prefix += " from " + p.FromRole
		}
		prefix += "] "
		res, err := a.Agents.Run(ctx, agent.RunRequest{
			AgentID:     toSess.AgentID,
			SessionID:   p.ToSessionID,
			WorkspaceID: toSess.WorkspaceID,
			UserMessage: prefix + p.Content,
		})
		return core.MustJSON(res), err

	// ── Agent role management ───────────────────────────────────────────────

	case protocol.MethodMapStatus, protocol.MethodMapBuild, protocol.MethodMapGraph,
		protocol.MethodMapQuery, protocol.MethodMapNeighbors,
		protocol.MethodMapImpact, protocol.MethodMapShare:
		return s.handleMap(ctx, req)

	case protocol.MethodCtxGet, protocol.MethodCtxPlace, protocol.MethodCtxRemove, protocol.MethodCtxLink,
		protocol.MethodCtxUpdateLink, protocol.MethodCtxUnlink, protocol.MethodCtxSend, protocol.MethodCtxRelays, protocol.MethodCtxAssistant:
		return s.handleCtx(ctx, req)

	case protocol.MethodConnectCatalog, protocol.MethodConnectTerminal, protocol.MethodConnectAdd:
		return s.handleConnect(ctx, req)

	case protocol.MethodPersonaCatalog, protocol.MethodPersonaGet, protocol.MethodPersonaSet,
		protocol.MethodPersonaClear, protocol.MethodPersonaBadges, protocol.MethodSessionCancel:
		return s.handlePersona(ctx, req)

	case protocol.MethodSubagentList, protocol.MethodSubagentStop, protocol.MethodSubagentSteer,
		protocol.MethodSubagentApply, protocol.MethodSubagentDiscard:
		return s.handleSubagents(ctx, req)

	case protocol.MethodWorkPlan, protocol.MethodWorkGet, protocol.MethodWorkRemovePhase, protocol.MethodWorkRemoveAgent,
		protocol.MethodWorkApprove, protocol.MethodWorkCancel, protocol.MethodWorkDiscard, protocol.MethodWorkRetry:
		return s.handleTeamwork(ctx, req)
	case protocol.MethodStaffMembers, protocol.MethodStaffAssign, protocol.MethodStaffTasks, protocol.MethodStaffSeen,
		protocol.MethodStaffStop, protocol.MethodStaffNoteAdd, protocol.MethodStaffNoteDelete,
		protocol.MethodStaffWatches, protocol.MethodStaffWatchSave, protocol.MethodStaffWatchDelete,
		protocol.MethodStaffSchedules, protocol.MethodStaffScheduleSave, protocol.MethodStaffScheduleDelete,
		protocol.MethodStaffDiff, protocol.MethodStaffApply, protocol.MethodStaffDiscard, protocol.MethodStaffPR,
		protocol.MethodStaffMonitors, protocol.MethodStaffMonitorSave, protocol.MethodStaffMonitorDelete, protocol.MethodStaffMonitorPresets,
		protocol.MethodStaffHandoffs, protocol.MethodStaffHandoffSave, protocol.MethodStaffHandoffDelete:
		return s.handleStaff(ctx, req)

	case protocol.MethodModelList, protocol.MethodSessionModel, protocol.MethodSessionSetModel:
		return s.handleModels(ctx, req)

	case protocol.MethodEditsList, protocol.MethodEditsAccept, protocol.MethodEditsRevert,
		protocol.MethodSessionCompact, protocol.MethodSessionSetEffort, protocol.MethodWorkspaceRules:
		return s.handleReview(ctx, req)

	default:
		return nil, fmt.Errorf("unknown method %s", req.Method)
	}
}

// placeholderTitle is a title nobody chose: what a new chat starts with in
// any client language.
func placeholderTitle(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "untitled", "new chat", "new session", "chat", "yeni sohbet", "yeni oturum", "sohbet":
		return true
	}
	return false
}

// sessionTitleFrom turns a first message into a short topic line: context
// blocks, code and links are dropped, then the first sentence is cut at a
// word boundary. Empty when nothing readable is left (e.g. a slash command).
func sessionTitleFrom(content string) string {
	s := content
	for _, tag := range []string{"codebase-context", "context"} {
		for {
			i := strings.Index(s, "<"+tag+">")
			j := strings.Index(s, "</"+tag+">")
			if i < 0 || j < i {
				break
			}
			s = s[:i] + s[j+len(tag)+3:]
		}
	}
	for {
		i := strings.Index(s, "```")
		if i < 0 {
			break
		}
		j := strings.Index(s[i+3:], "```")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + " " + s[i+3+j+3:]
	}
	var words []string
	for _, w := range strings.Fields(s) {
		if strings.HasPrefix(w, "http://") || strings.HasPrefix(w, "https://") || strings.HasPrefix(w, "@") || (len(words) == 0 && strings.HasPrefix(w, "/")) {
			continue
		}
		words = append(words, w)
	}
	line := strings.Join(words, " ")
	if i := strings.IndexAny(line, ".?!\n"); i > 12 {
		line = line[:i]
	}
	line = strings.Trim(line, " .,;:-–—")
	runes := []rune(line)
	if len(runes) > 48 {
		cut := string(runes[:48])
		if k := strings.LastIndex(cut, " "); k > 24 {
			cut = cut[:k]
		}
		line = strings.TrimRight(cut, " .,;:-") + "…"
	}
	if line == "" {
		return ""
	}
	r := []rune(line)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// fallbackDir is where a chat with no folder of its own works: the daemon's
// directory — unless that is the disk root, which is what a desktop app
// started from the Finder or the Dock gets. Nobody means to hand an agent
// the whole disk; the home folder is where a shell would start instead.
func fallbackDir() string {
	cwd, err := os.Getwd()
	if err == nil && cwd != "" && filepath.Dir(cwd) != cwd {
		return cwd
	}
	if home, herr := os.UserHomeDir(); herr == nil && home != "" {
		return home
	}
	return cwd
}
