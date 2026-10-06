package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Kayra-ML/rove/internal/panes"
	"github.com/Kayra-ML/rove/pkg/protocol"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/approval"
	"github.com/Kayra-ML/rove/internal/automation"
	"github.com/Kayra-ML/rove/internal/checkpoint"
	"github.com/Kayra-ML/rove/internal/codemap"
	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/connect"
	"github.com/Kayra-ML/rove/internal/ctxmap"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/goal"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/index"
	"github.com/Kayra-ML/rove/internal/judge"
	"github.com/Kayra-ML/rove/internal/marketplace"
	"github.com/Kayra-ML/rove/internal/mcp"
	"github.com/Kayra-ML/rove/internal/memory"
	"github.com/Kayra-ML/rove/internal/permission"
	"github.com/Kayra-ML/rove/internal/persona"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/qualitygate"
	"github.com/Kayra-ML/rove/internal/secrets"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/skill"
	"github.com/Kayra-ML/rove/internal/ssh"
	"github.com/Kayra-ML/rove/internal/staff"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/team"
	"github.com/Kayra-ML/rove/internal/teamwork"
	"github.com/Kayra-ML/rove/internal/terminal"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/internal/webhook"
	"github.com/Kayra-ML/rove/internal/workspace"
)

// App is the Go Core. CLI and Desktop are clients of this object via the daemon.
type App struct {
	Cfg     config.Config
	Store   *store.Store
	Bus     *eventbus.Bus
	Secrets *secrets.Store
	Perm    *permission.Engine
	// Approvals holds the permission questions waiting for an answer.
	Approvals *approval.Gate
	Tools     *tool.Runtime
	Router    *provider.Router
	// AgentSessions keeps each chat's session with a connected agent system
	// (Claude Code, Codex…), so a turn resumes it instead of resending all.
	AgentSessions *provider.AgentSessions
	Agents        *agent.Runtime
	Sess          *session.Manager
	Mem           *memory.System
	Judge         *judge.Engine
	Goals         *goal.Engine
	Team          *team.Team
	// Subagents runs and tracks the work a chat hands to subagents.
	Subagents *team.Subagents
	Work      *teamwork.Engine
	// Staff runs the Agent space's team: tasks, reports, notes, colleagues.
	Staff *staff.Engine
	// Panes runs a session's terminals as one team (terminal mode).
	Panes   *panes.Hub
	Term    *terminal.Engine
	SSH     *ssh.Manager
	Git     *gitwt.Manager
	WS      *workspace.Manager
	Skills  *skill.Runtime
	Market  *marketplace.Catalog
	MCP     *mcp.Runtime
	Auto    *automation.Engine
	Webhook *webhook.Engine
	Token   string
	Checkpt *checkpoint.Manager
	Index   *index.Indexer
	Map     *codemap.Service
	Links   *ctxmap.Engine

	mu      sync.Mutex
	cancels []context.CancelFunc
	started time.Time
	// the program this daemon runs from, and when that file was written: the
	// desktop app compares them with the daemon it carries to tell whether
	// this one is older and should make way
	exe     string
	exeTime time.Time
}

func Open(cfg config.Config) (*App, error) {
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}
	st, err := store.Open(config.DBPath(cfg.DataDir))
	if err != nil {
		return nil, err
	}
	sec, err := secrets.Open(cfg.DataDir, nil)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	token, err := loadOrCreateToken(cfg.DataDir)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	bus := eventbus.New()
	perm := permission.New(st)
	tools := tool.New(perm)
	// A rule that says "ask" is a question for the user, not a refusal: the
	// run waits while the app puts it on screen.
	gate := approval.New(bus)
	gate.Remember = func(ctx context.Context, r approval.Request, allow bool) error {
		d := types.PermDeny
		if allow {
			d = types.PermAllow
		}
		rule := types.PermissionRule{Action: r.Action, Pattern: "*", Decision: d}
		// Turn the blanket rule for this action around rather than adding a
		// second one beside it: two rules that match equally well are
		// settled by order, so a new one would not be heard.
		if rules, err := st.ListPermissions(ctx); err == nil {
			for _, old := range rules {
				if old.Action == r.Action && old.Pattern == "*" && old.Skill == "" && old.AgentID == "" {
					rule.ID = old.ID
					break
				}
			}
		}
		return perm.Put(ctx, rule)
	}
	tools.SetApprover(func(ctx context.Context, tc tool.Context, name string, action types.PermissionAction, detail string) (bool, error) {
		return gate.Ask(ctx, approval.Request{SessionID: tc.SessionID, Action: action, Tool: name, Detail: detail})
	})
	git := gitwt.New()
	tools.Register(tool.ReadFile{})
	tools.Register(tool.WriteFile{})
	tools.Register(tool.PatchFile{})
	tools.Register(tool.ListDir{})
	tools.Register(tool.Shell{})
	tools.Register(tool.GitStatusTool{Git: git})
	tools.Register(tool.GitCommitTool{Git: git})
	cmap := codemap.NewService(filepath.Join(cfg.DataDir, "codemap"))
	for _, t := range codemap.Tools(cmap) {
		tools.Register(t)
	}
	router := provider.NewRouter()
	if raw, err := st.GetKV(context.Background(), "usage"); err == nil && raw != "" {
		var snap provider.MeterSnapshot
		if json.Unmarshal([]byte(raw), &snap) == nil {
			router.Meter.Load(snap)
		}
	}
	router.Meter.SetPersist(func(s provider.MeterSnapshot) {
		b, _ := json.Marshal(s)
		_ = st.PutKV(context.Background(), "usage", string(b))
	})
	if _, err := st.GetKV(context.Background(), "firstSeen"); err != nil {
		_ = st.PutKV(context.Background(), "firstSeen", time.Now().UTC().Format(time.RFC3339))
	}
	router.Register("fake", &provider.Fake{NameVal: "fake", Responses: []string{
		"I examined the workspace and applied the next increment. hello",
	}})
	sess := session.New(st, bus)
	mem := memory.New(st)
	agents := agent.New(st, bus, sess, mem, router, tools)
	agents.SetCheckpointManager(checkpoint.New())
	agents.SetEditHook(func(workspace string, paths []string) {
		_, _ = cmap.Touch(workspace, paths)
	})
	links := ctxmap.New(st, bus, agents, ctxmap.DefaultFinder{Map: cmap})
	for _, t := range ctxmap.Tools(links) {
		tools.Register(t)
	}
	// long chats are summarized before they are sent again (token budget)
	agents.CompactAt = 40000
	agents.KeepTurns = 4
	crew := team.New(st, sess)
	// Subagents, after Hermes Agent's delegate_task: any chat can hand work
	// to fresh agents; parallel ones get a checkout each (after Orca) that
	// lives outside the user's repo, so it never shows in their git status.
	subs := &team.Subagents{
		Team: crew, Run: agents.Run,
		Roster:       team.NewRoster(bus),
		Git:          gitwt.New(),
		WorktreeRoot: filepath.Join(cfg.DataDir, "worktrees"),
		Cancel:       agents.CancelSession,
		Steer:        agents.Steer,
	}
	tools.Register(team.Delegate{Subagents: subs})
	// a session's terminals: one board, one set of file claims
	hub := panes.New(st, sess)
	hub.Run = agents.Run
	hub.Running = agents.Running
	hub.RoleOf = func(ctx context.Context, id types.ID) (string, string) {
		if r, err := persona.Resolve(ctx, st, id); err == nil && r.CharacterID != "" {
			if c, ok := persona.CharacterByID(r.CharacterID); ok {
				if en, ok := c.I18n["en"]; ok && en.Name != "" {
					return c.ID, en.Name // the board is read by models: plain English
				}
				return c.ID, c.Name
			}
		}
		return "", ""
	}
	agents.SetClaims(hub)
	tools.Register(panes.SendTool{Hub: hub})
	// the Agent space's agents work like a team: titles, notes of their own,
	// tasks handed out with a report back, and colleagues to ask
	crewOf := staff.New(st, sess, bus)
	crewOf.Run = agents.Run
	crewOf.Ask = agents.Ask
	// each task works in a checkout of its own, kept for the user to review
	crewOf.Git = gitwt.New()
	crewOf.WorktreeRoot = filepath.Join(cfg.DataDir, "worktrees", "agents")
	crewOf.Running = agents.Running
	crewOf.Cancel = agents.CancelSession
	crewOf.DefaultAgent = func(ctx context.Context) (types.ID, error) {
		a, err := agents.EnsureDefault(ctx)
		return a.ID, err
	}
	// a finished turn feeds the Session Map: cables to other chats, and the
	// agents watching the chat
	agents.SetRunHook(func(req agent.RunRequest, res agent.RunResult) {
		links.OnRunDone(req, res)
		crewOf.OnRunDone(req, res)
	})
	tools.Register(staff.NoteTool{E: crewOf})
	tools.Register(staff.AskTool{E: crewOf})
	// a chat sets up automations when asked to: watches, cables, schedules
	for _, tl := range staff.AutomationTools(crewOf) {
		tools.Register(tl)
	}
	crewOf.Start()
	agents.SetPersonaSource(func(ctx context.Context, sessionID types.ID) agent.Persona {
		r, err := persona.Resolve(ctx, st, sessionID)
		if err != nil {
			r = persona.Resolved{}
		}
		p := agent.Persona{Prompt: r.Prompt, Model: r.Model, Provider: r.Provider}
		// a model picked for this chat (/models) wins over the profile's
		if m, err := st.GetSessionModel(ctx, sessionID); err == nil && m.Model != "" {
			p.Model, p.Provider = m.Model, m.Provider
		}
		p.Effort = st.GetSessionEffort(ctx, sessionID)
		// Every chat learns how to hand work to subagents and gets the tool;
		// a channel — a subagent's, a terminal's — never does, so delegation
		// stays one level deep.
		brief := crew.Brief(ctx, sessionID)
		// terminals of one session see what the others do
		board := hub.Board(ctx, sessionID)
		// an Agent-space agent knows who it is, its notes and its colleagues
		self := crewOf.Brief(ctx, sessionID)
		p.Context = strings.TrimSpace(strings.Join([]string{self, brief, board}, "\n\n"))
		p.Allow = func(name string) bool {
			if name == team.DelegateName {
				return brief != ""
			}
			if name == staff.NoteName {
				return self != ""
			}
			if name == staff.AskName {
				return self != "" && crewOf.CanAsk(ctx, sessionID)
			}
			// only a chat the user talks in sets up automations: never a
			// subagent, a terminal pane or an agent's own task
			if staff.IsAutomationTool(name) {
				return crewOf.CanAutomate(ctx, sessionID)
			}
			if name == panes.SendName {
				return board != ""
			}
			return (r.Features == nil || r.AllowTool(name)) && r.AllowIntegration(name)
		}
		return p
	})
	j := judge.New(qualitygate.New())
	// the goal reviewer is a tool-less call on the goal agent's model
	j.Ask = agents.Ask
	ws := workspace.New(st, git)
	crewOf.WorkspacePath = func(ctx context.Context, id types.ID) string {
		if w, err := ws.Get(ctx, id); err == nil {
			return w.Path
		}
		return ""
	}
	work := teamwork.New(st, sess, bus)
	work.Ask = agents.AskModel
	work.ChatModel = func(ctx context.Context, sessionID types.ID) types.ModelRef {
		m, _ := st.GetSessionModel(ctx, sessionID)
		return m
	}
	work.Run = agents.Run
	work.Cat = catalogOf{}
	work.Characters = func() []teamwork.CharacterLine {
		out := make([]teamwork.CharacterLine, 0, len(persona.Characters))
		for _, c := range persona.Characters {
			// the planner reads English: plain and cheap for any model
			name, sum := c.Name, c.Summary
			if en, ok := c.I18n["en"]; ok {
				name, sum = en.Name, en.Summary
			}
			out = append(out, teamwork.CharacterLine{ID: c.ID, Name: name, Summary: sum})
		}
		return out
	}
	work.Name = func(id string) string {
		if c, ok := persona.CharacterByID(id); ok {
			return c.Name
		}
		return id
	}
	hub.WorkspacePath = func(ctx context.Context, id types.ID) string {
		if w, err := ws.Get(ctx, id); err == nil {
			return w.Path
		}
		return ""
	}
	work.WorkspacePath = func(ctx context.Context, id types.ID) string {
		if w, err := ws.Get(ctx, id); err == nil {
			return w.Path
		}
		return ""
	}
	goals := goal.New(st, bus, agents, j, ws)
	goals.ToolNames = func() []string {
		specs := tools.Specs()
		out := make([]string, 0, len(specs))
		for _, sp := range specs {
			out = append(out, sp.Name)
		}
		return out
	}
	goals.Models = func(ctx context.Context) []types.ModelRef {
		provs, err := st.ListProviders(ctx)
		if err != nil {
			return nil
		}
		var out []types.ModelRef
		for _, p := range provs {
			for _, m := range p.Models {
				out = append(out, types.ModelRef{Provider: p.Name, Model: m})
			}
		}
		return out
	}
	snapshots := checkpoint.New()
	goals.Snapshot = func(dir, label string) error {
		_, err := snapshots.Take(dir, label)
		return err
	}
	auto := automation.New(st, bus, goals)
	term := terminal.New(st, bus)
	sshMgr := ssh.New(term)
	sk := skill.New(st, filepath.Join(cfg.DataDir, "skills"))
	market := marketplace.New(filepath.Join(cfg.DataDir, "registry"), sk)
	mcpRt := mcp.New(tools)
	wh := webhook.New(st)

	// Codebase FTS5 indexer (best-effort — failure is non-fatal).
	idx, _ := index.New(filepath.Join(cfg.DataDir, "codebase.db"))

	app := &App{
		Cfg: cfg, Store: st, Bus: bus, Secrets: sec, Perm: perm, Tools: tools, Router: router,
		Agents: agents, Sess: sess, Mem: mem, Judge: j, Goals: goals, Team: crew, Subagents: subs, Work: work, Staff: crewOf, Panes: hub,
		Approvals: gate,
		Term:      term, SSH: sshMgr, Git: git, WS: ws, Skills: sk, Market: market, MCP: mcpRt,
		Auto: auto, Webhook: wh, Token: token, Checkpt: checkpoint.New(), Index: idx, Map: cmap, Links: links,
		AgentSessions: provider.NewAgentSessions(filepath.Join(cfg.DataDir, "agent-sessions.json")),
		started:       time.Now().UTC(),
	}
	if exe, err := os.Executable(); err == nil {
		app.exe = exe
		if fi, err := os.Stat(exe); err == nil {
			app.exeTime = fi.ModTime()
		}
	}
	if err := app.seed(context.Background()); err != nil {
		app.Close()
		return nil, err
	}
	app.recover(context.Background())
	auto.Start(context.Background())
	go app.startMCP()
	app.cancels = append(app.cancels, auto.Stop)
	return app, nil
}

func (a *App) seed(ctx context.Context) error {
	if _, err := a.Agents.EnsureDefault(ctx); err != nil {
		return err
	}
	providers, err := a.Store.ListProviders(ctx)
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		_ = a.Store.UpsertProvider(ctx, types.Provider{
			ID: id.NewID(), Name: "fake", Kind: types.ProviderFake, Models: []string{"fake"}, Default: true,
		})
	}
	if err := a.Perm.Put(ctx, types.PermissionRule{
		ID: "seed-fs", Action: types.PermFilesystem, Pattern: "*", Decision: types.PermAllow,
	}); err != nil {
		return err
	}
	if err := a.Perm.Put(ctx, types.PermissionRule{
		ID: "seed-shell", Action: types.PermShell, Pattern: "*", Decision: types.PermAsk,
	}); err != nil {
		return err
	}
	if err := a.Perm.Put(ctx, types.PermissionRule{
		ID: "seed-git", Action: types.PermGit, Pattern: "*", Decision: types.PermAllow,
	}); err != nil {
		return err
	}
	// jobs of kinds an older version had, which nothing runs any more
	if a.Auto != nil {
		if jobs, err := a.Auto.List(ctx); err == nil {
			for _, j := range jobs {
				switch j.Kind {
				case "sweep_ready", "assign_idle", "stale_card_nudge":
					_ = a.Auto.Delete(ctx, j.ID)
				}
			}
		}
	}
	return a.hydrateProviders(ctx)
}

func (a *App) hydrateProviders(ctx context.Context) error {
	list, err := a.Store.ListProviders(ctx)
	if err != nil {
		return err
	}
	for _, p := range list {
		p.Kind = types.NormalizeProviderKind(p.Kind)
		switch p.Kind {
		case types.ProviderFake:
			a.Router.Register(p.Name, &provider.Fake{NameVal: p.Name, Responses: []string{"ok"}})
		case types.ProviderAgentCLI:
			system, access, ok := provider.ParseAgentURL(p.BaseURL)
			if !ok {
				continue
			}
			a.Router.Register(p.Name, &provider.AgentCLI{NameVal: p.Name, System: system, Access: access, Sessions: a.AgentSessions})
			// the account's models change as the system updates its list:
			// pick that up whenever providers are loaded
			if models := connect.AccountModels(system); !slices.Equal(models, p.Models) {
				p.Models = models
				_ = a.Store.UpsertProvider(ctx, p)
			}
		case types.ProviderOpenAICompat, types.ProviderAnthropic:
			base := strings.TrimSpace(p.BaseURL)
			if base == "" {
				base = "https://api.openai.com/v1"
			}
			a.Router.Register(p.Name, &provider.OpenAICompat{
				NameVal:  p.Name,
				BaseURL:  base,
				SecretID: p.SecretID,
				Keys:     a.Secrets.Get,
			})
		}
		if p.Default && p.Kind != types.ProviderFake {
			a.Router.SetDefault("default", p.Name)
			_ = a.retargetFakeAgents(ctx, p)
		}
	}
	return nil
}

func (a *App) retargetFakeAgents(ctx context.Context, p types.Provider) error {
	if a.Agents == nil {
		return nil
	}
	if len(p.Models) == 0 {
		return nil // nothing known to point agents at yet
	}
	model := p.Models[0]
	agents, err := a.Agents.List(ctx)
	if err != nil {
		return err
	}
	for _, ag := range agents {
		if ag.Provider != "" && ag.Provider != "fake" && ag.Model != "" && ag.Model != "fake" {
			continue
		}
		ag.Provider = p.Name
		ag.Model = model
		if _, err := a.Agents.Upsert(ctx, ag); err != nil {
			return err
		}
	}
	return nil
}

// ProviderResult is a saved provider and how its model list went: fetched
// from the provider, or why that failed.
type ProviderResult struct {
	types.Provider
	ModelsFetched int    `json:"modelsFetched,omitempty"`
	ModelsError   string `json:"modelsError,omitempty"`
}

// FetchProviderModels asks a provider for its models, with its stored key.
func (a *App) FetchProviderModels(ctx context.Context, p types.Provider) ([]string, error) {
	// an agent system offers the models of the account it is signed in to,
	// by the names it takes; "default" is whatever the account uses
	if types.NormalizeProviderKind(p.Kind) == types.ProviderAgentCLI {
		system, _, _ := provider.ParseAgentURL(p.BaseURL)
		return connect.AccountModels(system), nil
	}
	key := ""
	if p.SecretID != "" && a.Secrets != nil {
		key, _ = a.Secrets.Get(p.SecretID)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return provider.FetchModels(ctx, nil, p.Kind, p.BaseURL, key)
}

// SaveProvider saves a provider; with no models given it fetches them from
// the provider itself ({base}/models).
func (a *App) SaveProvider(ctx context.Context, p types.Provider) (ProviderResult, error) {
	var res ProviderResult
	p.Kind = types.NormalizeProviderKind(p.Kind)
	if len(p.Models) == 0 && p.Kind != types.ProviderFake && p.Kind != "" {
		if models, err := a.FetchProviderModels(ctx, p); err == nil {
			p.Models = models
			res.ModelsFetched = len(models)
		} else {
			res.ModelsError = err.Error()
		}
	}
	saved, err := a.ApplyProvider(ctx, p)
	res.Provider = saved
	return res, err
}

// RefreshProviderModels replaces a provider's model list with what it
// serves now, and moves agents whose model is gone to one that exists.
func (a *App) RefreshProviderModels(ctx context.Context, id types.ID) (ProviderResult, error) {
	var res ProviderResult
	list, err := a.Store.ListProviders(ctx)
	if err != nil {
		return res, err
	}
	var p *types.Provider
	for i := range list {
		if list[i].ID == id {
			p = &list[i]
		}
	}
	if p == nil {
		return res, fmt.Errorf("provider not found")
	}
	models, err := a.FetchProviderModels(ctx, *p)
	if err != nil {
		return res, err
	}
	p.Models = models
	if err := a.Store.UpsertProvider(ctx, *p); err != nil {
		return res, err
	}
	if err := a.hydrateProviders(ctx); err != nil {
		return res, err
	}
	if err := a.fixAgentModels(ctx, *p); err != nil {
		return res, err
	}
	res.Provider, res.ModelsFetched = *p, len(models)
	return res, nil
}

// fixAgentModels moves the provider's agents off models it no longer lists
// (e.g. a placeholder saved before the real list was known).
func (a *App) fixAgentModels(ctx context.Context, p types.Provider) error {
	if a.Agents == nil || len(p.Models) == 0 {
		return nil
	}
	have := map[string]bool{}
	for _, m := range p.Models {
		have[m] = true
	}
	agents, err := a.Agents.List(ctx)
	if err != nil {
		return err
	}
	for _, ag := range agents {
		if ag.Provider != p.Name || have[ag.Model] {
			continue
		}
		ag.Model = p.Models[0]
		if _, err := a.Agents.Upsert(ctx, ag); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) ApplyProvider(ctx context.Context, p types.Provider) (types.Provider, error) {
	p.Kind = types.NormalizeProviderKind(p.Kind)
	if p.Name == "" {
		p.Name = "openai"
	}
	if p.Kind == "" {
		p.Kind = types.ProviderOpenAICompat
	}
	if p.BaseURL == "" && p.Kind == types.ProviderOpenAICompat {
		p.BaseURL = "https://api.openai.com/v1"
	}
	// no list (the provider could not be asked): OpenAI's own endpoint
	// gets its usual model; any other provider stays empty rather than
	// pointing agents at a model it may not serve
	if len(p.Models) == 0 && strings.Contains(p.BaseURL, "api.openai.com") {
		p.Models = []string{"gpt-4o"}
	}
	if p.SecretID == "" {
		p.SecretID = p.Name + "-key"
	}
	if p.ID == "" {
		p.ID = id.NewID()
	}
	if !p.Default {
		existing, err := a.Store.ListProviders(ctx)
		if err != nil {
			return p, err
		}
		hasRealDefault := false
		for _, cur := range existing {
			if cur.Default && types.NormalizeProviderKind(cur.Kind) != types.ProviderFake && cur.ID != p.ID {
				hasRealDefault = true
				break
			}
		}
		if !hasRealDefault && p.Kind != types.ProviderFake {
			p.Default = true
		}
	}
	if err := a.Store.UpsertProvider(ctx, p); err != nil {
		return p, err
	}
	if err := a.hydrateProviders(ctx); err != nil {
		return p, err
	}
	return p, a.fixAgentModels(ctx, p)
}

func (a *App) recover(ctx context.Context) {
	// a task the daemon was working on when it stopped did not finish
	if a.Staff != nil {
		a.Staff.Recover(ctx)
	}
	goals, err := a.Goals.List(ctx)
	if err != nil {
		return
	}
	for _, g := range goals {
		if g.Status == types.GoalRunning {
			g.Status = types.GoalPending
			g.UpdatedAt = time.Now().UTC()
			_ = a.Store.UpsertGoal(ctx, g)
		}
	}
}

func (a *App) Close() error {
	a.mu.Lock()
	for _, c := range a.cancels {
		c()
	}
	a.mu.Unlock()
	if a.MCP != nil {
		a.MCP.StopAll()
	}
	if a.Auto != nil {
		a.Auto.Stop()
	}
	if a.Goals != nil {
		a.Goals.Close()
	}
	if a.Staff != nil {
		a.Staff.Close()
	}
	if a.Bus != nil {
		a.Bus.Close()
	}
	if a.Index != nil {
		_ = a.Index.Close()
	}
	if a.Map != nil {
		a.Map.Flush()
	}
	if a.Store != nil {
		return a.Store.Close()
	}
	return nil
}

func loadOrCreateToken(dir string) (string, error) {
	p := config.TokenPath(dir)
	b, err := os.ReadFile(p)
	if err == nil && len(b) >= 16 {
		return string(b), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.WriteFile(p, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

func (a *App) Health() map[string]any {
	var snap provider.MeterSnapshot
	if a.Router != nil {
		snap = a.Router.Meter.Snapshot()
	}
	running := 0
	if a.Agents != nil {
		if list, err := a.Agents.List(context.Background()); err == nil {
			for _, ag := range list {
				if ag.Status == types.AgentRunning {
					running++
				}
			}
		}
	}
	firstSeen := a.started
	if a.Store != nil {
		if raw, err := a.Store.GetKV(context.Background(), "firstSeen"); err == nil && raw != "" {
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				firstSeen = t
			}
		}
	}
	days := int(time.Since(firstSeen).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return map[string]any{
		"ok":               true,
		"uptime":           time.Since(a.started).String(),
		"uptimeSeconds":    int64(time.Since(a.started).Seconds()),
		"startedAt":        a.started.UTC().Format(time.RFC3339),
		"firstSeen":        firstSeen.UTC().Format(time.RFC3339),
		"days":             days,
		"dataDir":          a.Cfg.DataDir,
		"promptTokens":     snap.PromptTokens,
		"completionTokens": snap.CompletionTokens,
		"totalTokens":      snap.TotalTokens,
		"calls":            snap.Calls,
		"cachedTokens":     snap.CachedTokens,
		"apiLevel":         protocol.APILevel,
		"exe":              a.exe,
		"exeModTime":       a.exeTime.Unix(),
		"activeAgents":     running,
	}
}

func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func (a *App) FileTree(root string, max int) ([]string, error) {
	if max <= 0 {
		max = 500
	}
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		base := info.Name()
		if info.IsDir() && (base == ".git" || base == "node_modules" || base == ".aether" || base == "vendor" || base == "dist" || base == "build" || base == ".next") {
			return filepath.SkipDir
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			rel += "/"
		}
		out = append(out, rel)
		if len(out) >= max {
			return fmt.Errorf("max")
		}
		return nil
	})
	if err != nil && err.Error() != "max" {
		return out, err
	}
	return out, nil
}

// catalogOf lets Teamwork check character ids against the persona catalog.
type catalogOf struct{}

func (catalogOf) Has(id string) bool {
	_, ok := persona.CharacterByID(id)
	return ok
}

// startMCP runs the MCP servers the user added, so their tools reach the
// agents. A server that fails to start is noted and skipped.
func (a *App) startMCP() {
	ctx := context.Background()
	list, err := a.Store.ListMCPServers(ctx)
	if err != nil {
		return
	}
	for _, srv := range list {
		a.StartMCP(ctx, srv)
	}
}

// StartMCP runs one server, replacing one of the same name.
func (a *App) StartMCP(ctx context.Context, srv types.MCPServerConfig) error {
	if a.MCP == nil {
		return nil
	}
	err := a.MCP.Start(ctx, mcp.ServerConfig{Name: srv.Name, Command: srv.Command, Args: srv.Args, Env: srv.Env})
	if err != nil {
		log.Printf("mcp %s: %v", srv.Name, err)
	}
	return err
}
