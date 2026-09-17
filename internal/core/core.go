package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aether-dev/aether/internal/agent"
	"github.com/aether-dev/aether/internal/automation"
	"github.com/aether-dev/aether/internal/checkpoint"
	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/internal/eventbus"
	"github.com/aether-dev/aether/internal/gitwt"
	"github.com/aether-dev/aether/internal/goal"
	"github.com/aether-dev/aether/internal/id"
	"github.com/aether-dev/aether/internal/index"
	"github.com/aether-dev/aether/internal/judge"
	"github.com/aether-dev/aether/internal/kanban"
	"github.com/aether-dev/aether/internal/lease"
	"github.com/aether-dev/aether/internal/marketplace"
	"github.com/aether-dev/aether/internal/mcp"
	"github.com/aether-dev/aether/internal/memory"
	"github.com/aether-dev/aether/internal/orchestrator"
	"github.com/aether-dev/aether/internal/permission"
	"github.com/aether-dev/aether/internal/provider"
	"github.com/aether-dev/aether/internal/qualitygate"
	"github.com/aether-dev/aether/internal/secrets"
	"github.com/aether-dev/aether/internal/session"
	"github.com/aether-dev/aether/internal/skill"
	"github.com/aether-dev/aether/internal/ssh"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/terminal"
	"github.com/aether-dev/aether/internal/tool"
	"github.com/aether-dev/aether/internal/types"
	"github.com/aether-dev/aether/internal/webhook"
	"github.com/aether-dev/aether/internal/workspace"
)

// App is the Go Core. CLI and Desktop are clients of this object via the daemon.
type App struct {
	Cfg     config.Config
	Store   *store.Store
	Bus     *eventbus.Bus
	Secrets *secrets.Store
	Perm    *permission.Engine
	Tools   *tool.Runtime
	Router  *provider.Router
	Agents  *agent.Runtime
	Sess    *session.Manager
	Mem     *memory.System
	Kanban  *kanban.Engine
	Judge   *judge.Engine
	Goals   *goal.Engine
	Orch    *orchestrator.Orchestrator
	Term    *terminal.Engine
	SSH     *ssh.Manager
	Git     *gitwt.Manager
	WS      *workspace.Manager
	Skills  *skill.Runtime
	Market  *marketplace.Catalog
	MCP     *mcp.Runtime
	Leases  *lease.Coordinator
	Auto    *automation.Engine
	Webhook *webhook.Engine
	Token   string
	Checkpt *checkpoint.Manager
	Index   *index.Indexer

	mu      sync.Mutex
	cancels []context.CancelFunc
	started time.Time
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
	git := gitwt.New()
	tools.Register(tool.ReadFile{})
	tools.Register(tool.WriteFile{})
	tools.Register(tool.PatchFile{})
	tools.Register(tool.ListDir{})
	tools.Register(tool.Shell{})
	tools.Register(tool.GitStatusTool{Git: git})
	tools.Register(tool.GitCommitTool{Git: git})
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
	k := kanban.New(st, bus)
	j := judge.New(qualitygate.New())
	ws := workspace.New(st, git)
	goals := goal.New(st, bus, k, agents, j, git, ws)
	leases := lease.New(st, 15*time.Minute)
	orch := orchestrator.New(st, bus, k, agents, goals, sess, leases, git, ws)
	auto := automation.New(st, bus, k, orch, goals, agents)
	term := terminal.New(st, bus)
	sshMgr := ssh.New(term)
	sk := skill.New(st, filepath.Join(cfg.DataDir, "skills"))
	market := marketplace.New(filepath.Join(cfg.DataDir, "registry"), sk)
	mcpRt := mcp.New(tools)

	// Codebase FTS5 indexer (best-effort — failure is non-fatal).
	idx, _ := index.New(filepath.Join(cfg.DataDir, "codebase.db"))

	app := &App{
		Cfg: cfg, Store: st, Bus: bus, Secrets: sec, Perm: perm, Tools: tools, Router: router,
		Agents: agents, Sess: sess, Mem: mem, Kanban: k, Judge: j, Goals: goals, Orch: orch,
		Term: term, SSH: sshMgr, Git: git, WS: ws, Skills: sk, Market: market, MCP: mcpRt,
		Leases: leases, Auto: auto, Token: token, Checkpt: checkpoint.New(), Index: idx,
		started: time.Now().UTC(),
	}
	if err := app.seed(context.Background()); err != nil {
		app.Close()
		return nil, err
	}
	app.recover(context.Background())
	auto.Start(context.Background())
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
	if a.Auto != nil {
		if jobs, err := a.Auto.List(ctx); err == nil && len(jobs) == 0 {
			_, _ = a.Auto.Upsert(ctx, types.AutomationJob{
				Name: "sweep ready", Kind: types.AutoSweepReady, EverySeconds: 30, Enabled: true,
			})
			_, _ = a.Auto.Upsert(ctx, types.AutomationJob{
				Name: "assign idle", Kind: types.AutoAssignIdle, EverySeconds: 60, Enabled: false,
			})
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
		switch p.Kind {
		case types.ProviderFake:
			a.Router.Register(p.Name, &provider.Fake{NameVal: p.Name, Responses: []string{"ok"}})
		case types.ProviderOpenAICompat:
			a.Router.Register(p.Name, &provider.OpenAICompat{
				NameVal:  p.Name,
				BaseURL:  p.BaseURL,
				SecretID: p.SecretID,
				Keys:     a.Secrets.Get,
			})
		}
		if p.Default {
			a.Router.SetDefault("default", p.Name)
		}
	}
	return nil
}

func (a *App) recover(ctx context.Context) {
	cards, err := a.Kanban.List(ctx, "")
	if err != nil {
		return
	}
	for _, c := range cards {
		if c.Column == types.ColRunning {
			c.Status = "interrupted"
			_, _ = a.Kanban.Move(ctx, c.ID, types.ColReady)
			_ = a.Kanban.AppendLog(ctx, c.ID, types.LogEntry{Level: "warn", Message: "recovered after daemon restart", Source: "core"})
		}
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
	if a.Auto != nil {
		a.Auto.Stop()
	}
	if a.Bus != nil {
		a.Bus.Close()
	}
	if a.Index != nil {
		_ = a.Index.Close()
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
