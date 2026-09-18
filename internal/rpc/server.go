package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aether-dev/aether/internal/agent"
	"github.com/aether-dev/aether/internal/core"
	"github.com/aether-dev/aether/internal/harness"
	"github.com/aether-dev/aether/internal/id"
	"github.com/aether-dev/aether/internal/marketplace"
	"github.com/aether-dev/aether/internal/orchestrator"
	"github.com/aether-dev/aether/internal/terminal"
	"github.com/aether-dev/aether/internal/types"
	"github.com/aether-dev/aether/pkg/protocol"
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

func (s *Server) ServeHTTP(addr string) error {
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
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s.http.ListenAndServe()
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

func (s *Server) handleHTTPRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
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
	if tok != s.app.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	filter := r.URL.Query().Get("filter")
	ch := make(chan types.Event, 64)
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
		"wails://wails":        {},
		"http://localhost":     {},
		"http://localhost:34115": {},
		"http://127.0.0.1":    {},
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
	if req.Method != protocol.MethodPing && req.Token != s.app.Token {
		return protocol.Response{ID: req.ID, OK: false, Error: "unauthorized"}
	}
	res, err := s.handle(ctx, req)
	if err != nil {
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
			Title       string   `json:"title"`
			AgentID     types.ID `json:"agentId"`
			WorkspaceID types.ID `json:"workspaceId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		out, err := a.Sess.Create(ctx, p.Title, p.AgentID, p.WorkspaceID)
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
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Workspace == "" && p.WorkspaceID != "" {
			if ws, err := a.WS.Get(ctx, p.WorkspaceID); err == nil {
				p.Workspace = ws.Path
			}
		}
		res, err := a.Agents.Run(ctx, agent.RunRequest{
			AgentID:     p.AgentID,
			SessionID:   p.SessionID,
			WorkspaceID: p.WorkspaceID,
			Workspace:   p.Workspace,
			UserMessage: p.Content,
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
	case protocol.MethodAgentCancel:
		var p struct {
			AgentID types.ID `json:"agentId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		a.Agents.Cancel(p.AgentID)
		return core.MustJSON(map[string]any{"ok": true}), nil
	case protocol.MethodCardList:
		var p struct {
			WorkspaceID types.ID `json:"workspaceId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		out, err := a.Kanban.List(ctx, p.WorkspaceID)
		return core.MustJSON(out), err
	case protocol.MethodCardCreate:
		var c types.Card
		if err := json.Unmarshal(req.Params, &c); err != nil {
			return nil, err
		}
		out, err := a.Kanban.Create(ctx, c)
		return core.MustJSON(out), err
	case protocol.MethodCardGet:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Kanban.Get(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodCardMove:
		var p struct {
			ID     types.ID           `json:"id"`
			Column types.KanbanColumn `json:"column"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Kanban.Move(ctx, p.ID, p.Column)
		return core.MustJSON(out), err
	case protocol.MethodCardReview:
		var p struct {
			ID    types.ID          `json:"id"`
			State types.ReviewState `json:"state"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Kanban.SetReview(ctx, p.ID, p.State)
		return core.MustJSON(out), err
	case protocol.MethodCardDispatch:
		var p struct {
			CardID    types.ID `json:"cardId"`
			SessionID types.ID `json:"sessionId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Orch.Dispatch(ctx, orchestrator.DispatchOpts{CardID: p.CardID, SessionID: p.SessionID})
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodCardAssign:
		var p struct {
			ID      types.ID `json:"id"`
			AgentID types.ID `json:"agentId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Kanban.Assign(ctx, p.ID, p.AgentID)
		return core.MustJSON(out), err
	case protocol.MethodCardDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Kanban.Delete(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
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
		go func() {
			_, _ = a.Goals.Drive(context.Background(), p.ID, p.SessionID, p.Workspace)
		}()
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
	case protocol.MethodTerminalKill:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Term.Kill(p.ID)
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
	case protocol.MethodSSHOpen:
		var p types.SSHTarget
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.SSH.Open(ctx, p, "", "")
		return core.MustJSON(out), err
	case protocol.MethodGitStatus:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		st, err := a.Git.Status(p.Path)
		return core.MustJSON(st), err
	case protocol.MethodGitDiff:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		diff, err := a.Git.Diff(p.Path)
		return core.MustJSON(map[string]any{"path": p.Path, "diff": diff}), err
	case protocol.MethodSkillList:
		out, err := a.Skills.List()
		return core.MustJSON(out), err
	case protocol.MethodSkillInstall:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Skills.InstallFromDir(p.Path)
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
	case protocol.MethodMarketPublish:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Market.PublishLocal(p.Path)
		return core.MustJSON(out), err
	case protocol.MethodMarketInstall:
		var p struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.Market.Install(p.Name, p.Version)
		return core.MustJSON(out), err
	case protocol.MethodProviderList:
		out, err := a.Store.ListProviders(ctx)
		return core.MustJSON(out), err
	case protocol.MethodProviderUpsert:
		var p types.Provider
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.ID == "" {
			p.ID = id.NewID()
		}
		if err := a.Store.UpsertProvider(ctx, p); err != nil {
			return nil, err
		}
		return core.MustJSON(p), nil
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
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out, err := a.FileTree(p.Path, 800)
		return core.MustJSON(out), err
	case protocol.MethodFileRead:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(p.Path)
		if err != nil {
			return nil, err
		}
		if len(b) > 256*1024 {
			b = b[:256*1024]
		}
		return core.MustJSON(map[string]any{"content": string(b)}), nil
	case protocol.MethodHealthStream:
		return core.MustJSON(a.Health()), nil
	case protocol.MethodShutdown:
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
		return core.MustJSON(map[string]any{"ok": true}), nil

	// ── Harness policy ────────────────────────────────────────────────────────
	case protocol.MethodHarnessGet:
		var p struct {
			GoalID string `json:"goalId"`
			CardID string `json:"cardId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		var raw string
		var lerr error
		if p.GoalID != "" {
			raw, lerr = a.Store.LoadGoalHarness(ctx, types.ID(p.GoalID))
		} else if p.CardID != "" {
			raw, lerr = a.Store.LoadCardHarness(ctx, types.ID(p.CardID))
		} else {
			return nil, fmt.Errorf("goalId or cardId required")
		}
		if lerr != nil {
			return nil, lerr
		}
		return json.RawMessage(raw), nil

	case protocol.MethodHarnessSet:
		var p struct {
			GoalID  string          `json:"goalId"`
			CardID  string          `json:"cardId"`
			Profile json.RawMessage `json:"profile"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		profileStr := string(p.Profile)
		if p.GoalID != "" {
			if err := a.Store.SaveGoalHarness(ctx, types.ID(p.GoalID), profileStr); err != nil {
				return nil, err
			}
		}
		if p.CardID != "" {
			if err := a.Store.SaveCardHarness(ctx, types.ID(p.CardID), profileStr); err != nil {
				return nil, err
			}
		}
		return core.MustJSON(map[string]any{"ok": true}), nil

	case protocol.MethodHarnessCompose:
		var p struct {
			Analysis harness.TaskAnalysis `json:"analysis"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		composer := harness.NewComposer()
		profile := composer.Compose(p.Analysis)
		now := time.Now().UTC()
		hp := harness.HarnessProfile{
			Current:   profile,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return core.MustJSON(hp), nil

	case protocol.MethodHarnessPresets:
		return core.MustJSON(map[string]any{
			"small": harness.SmallBugProfile(),
			"large": harness.LargeRefactorProfile(),
			"hard":  harness.HardLongTaskProfile(),
		}), nil

	case protocol.MethodHarnessMutations:
		var p struct {
			GoalID string `json:"goalId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.GoalID == "" {
			return nil, fmt.Errorf("goalId required")
		}
		rows, err := a.Store.ListHarnessMutations(ctx, types.ID(p.GoalID))
		if err != nil {
			return nil, err
		}
		return core.MustJSON(rows), nil

	case protocol.MethodCardLogs:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		c, err := a.Kanban.Get(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		return core.MustJSON(c.Logs), nil
	case protocol.MethodCardAddArtifact:
		var p struct {
			ID       types.ID       `json:"id"`
			Artifact types.Artifact `json:"artifact"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		c, err := a.Kanban.Get(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		c.Artifacts = append(c.Artifacts, p.Artifact)
		out, err := a.Kanban.Update(ctx, c)
		return core.MustJSON(out), err
	case protocol.MethodAgentDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.DeleteAgent(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodGitCommit:
		var p struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Git.CommitAll(p.Path, p.Message)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodGitLog:
		var p struct {
			Path  string `json:"path"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Limit <= 0 {
			p.Limit = 20
		}
		out, err := a.Git.Log(p.Path, p.Limit)
		return core.MustJSON(out), err
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
	case protocol.MethodCheckpointDrop:
		var p struct {
			Path string `json:"path"`
			Ref  string `json:"ref"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Checkpt.Drop(p.Path, p.Ref)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	// ── Diff hunk accept/reject ────────────────────────────────────────────
	case protocol.MethodGitApplyHunk:
		var p struct {
			Path  string `json:"path"`
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Git.ApplyHunk(p.Path, p.Patch)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodGitRejectHunk:
		var p struct {
			Path  string `json:"path"`
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Git.RejectHunk(p.Path, p.Patch)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

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

	case protocol.MethodSessionImport:
		var p struct {
			AgentID     types.ID `json:"agentId"`
			WorkspaceID types.ID `json:"workspaceId"`
			Title       string   `json:"title"`
			Markdown    string   `json:"markdown"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Title == "" {
			p.Title = "Imported session"
		}
		sess, err := a.Sess.Create(ctx, p.Title, p.AgentID, p.WorkspaceID)
		if err != nil {
			return nil, err
		}
		blocks := strings.Split(p.Markdown, "\n---\n")
		for _, block := range blocks {
			block = strings.TrimSpace(block)
			if block == "" {
				continue
			}
			lines := strings.SplitN(block, "\n", 3)
			if len(lines) < 2 {
				continue
			}
			roleLine := strings.TrimPrefix(strings.TrimSpace(lines[0]), "## ")
			var content string
			if len(lines) == 3 {
				content = strings.TrimSpace(lines[2])
			} else {
				content = strings.TrimSpace(lines[1])
			}
			if roleLine == "" || content == "" {
				continue
			}
			msg := types.Message{
				SessionID: sess.ID,
				Role:      types.MessageRole(roleLine),
				Content:   content,
			}
			if _, err := a.Sess.Append(ctx, msg); err != nil {
				return nil, err
			}
		}
		hist, herr := a.Sess.History(ctx, sess.ID)
		if herr != nil {
			return nil, herr
		}
		return core.MustJSON(map[string]any{"session": sess, "messages": hist}), nil

	// ── Git branch / push / PR ─────────────────────────────────────────────
	case protocol.MethodGitBranch:
		var p struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Git.CreateBranch(p.Path, p.Name)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodGitPush:
		var p struct {
			Path   string `json:"path"`
			Remote string `json:"remote"`
			Branch string `json:"branch"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Remote == "" {
			p.Remote = "origin"
		}
		err := a.Git.PushBranch(p.Path, p.Remote, p.Branch)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodGitPR:
		var p struct {
			Path  string `json:"path"`
			Title string `json:"title"`
			Body  string `json:"body"`
			Base  string `json:"base"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		url, err := a.Git.CreatePR(p.Path, p.Title, p.Body, p.Base)
		return core.MustJSON(map[string]any{"url": url}), err

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
		if srv.ID == "" {
			srv.ID = string(id.NewID())
		}
		if err := a.Store.UpsertMCPServer(ctx, srv); err != nil {
			return nil, err
		}
		return core.MustJSON(srv), nil

	case protocol.MethodMCPRemove:
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
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
		out, err := a.Store.UpsertAgentProfile(ctx, p)
		return core.MustJSON(out), err

	case protocol.MethodProfileDelete:
		var p struct {
			ID types.ID `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.DeleteAgentProfile(ctx, p.ID)
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

	case protocol.MethodProfileGetDefault:
		out, err := a.Store.GetDefaultProfile(ctx)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return core.MustJSON(map[string]any{"profile": nil}), nil
		}
		return core.MustJSON(map[string]any{"profile": out}), nil

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
	case protocol.MethodAgentSetRole:
		var p struct {
			AgentID types.ID        `json:"agentId"`
			Role    types.AgentRole `json:"role"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		err := a.Store.SetAgentRole(ctx, p.AgentID, p.Role)
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodAgentRoles:
		roles := []string{"leader", "frontend", "backend", "developer", "designer", "tester", "debugger", "reviewer", "researcher"}
		return core.MustJSON(roles), nil

	default:
		return nil, fmt.Errorf("unknown method %s", req.Method)
	}
}