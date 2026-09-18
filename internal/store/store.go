package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aether-dev/aether/internal/types"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	} else {
		dsn = "file:aether_mem?mode=memory&cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			profile TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			workspace_id TEXT NOT NULL DEFAULT '',
			system_prompt TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			agent_id TEXT NOT NULL DEFAULT '',
			workspace_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_calls TEXT NOT NULL DEFAULT '[]',
			tool_result TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS cards (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			column_name TEXT NOT NULL,
			assignee_agent_id TEXT NOT NULL DEFAULT '',
			profile TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			dependencies TEXT NOT NULL DEFAULT '[]',
			acceptance TEXT NOT NULL DEFAULT '[]',
			goal_id TEXT NOT NULL DEFAULT '',
			goal_mode INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			terminal_id TEXT NOT NULL DEFAULT '',
			git_branch TEXT NOT NULL DEFAULT '',
			worktree_path TEXT NOT NULL DEFAULT '',
			review_state TEXT NOT NULL DEFAULT '',
			artifacts TEXT NOT NULL DEFAULT '[]',
			logs TEXT NOT NULL DEFAULT '[]',
			workspace_id TEXT NOT NULL DEFAULT '',
			lease_holder TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cards_column ON cards(column_name)`,
		`ALTER TABLE cards ADD COLUMN session_id TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS goals (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			contract TEXT NOT NULL,
			status TEXT NOT NULL,
			card_id TEXT NOT NULL DEFAULT '',
			workspace_id TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			iteration INTEGER NOT NULL DEFAULT 0,
			last_verdict TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS verdicts (
			id TEXT PRIMARY KEY,
			goal_id TEXT NOT NULL,
			decision TEXT NOT NULL,
			reason TEXT NOT NULL,
			gate_results TEXT NOT NULL DEFAULT '[]',
			iteration INTEGER NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS workspaces (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			default_branch TEXT NOT NULL DEFAULT 'main',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS terminals (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			owner_id TEXT NOT NULL DEFAULT '',
			workspace_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			cwd TEXT NOT NULL DEFAULT '',
			shell TEXT NOT NULL DEFAULT '',
			cols INTEGER NOT NULL DEFAULT 80,
			rows INTEGER NOT NULL DEFAULT 24,
			pid INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			ssh TEXT,
			persistent INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			base_url TEXT NOT NULL DEFAULT '',
			models TEXT NOT NULL DEFAULT '[]',
			is_default INTEGER NOT NULL DEFAULT 0,
			secret_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS memory (
			id TEXT PRIMARY KEY,
			scope TEXT NOT NULL,
			scope_id TEXT NOT NULL DEFAULT '',
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(scope, scope_id, key)
		)`,
		`CREATE TABLE IF NOT EXISTS permission_rules (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			pattern TEXT NOT NULL,
			decision TEXT NOT NULL,
			skill TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS skills (
			name TEXT PRIMARY KEY,
			manifest TEXT NOT NULL,
			path TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'local',
			enabled INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS file_leases (
			path TEXT PRIMARY KEY,
			holder TEXT NOT NULL,
			card_id TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kv (
			k TEXT PRIMARY KEY,
			v TEXT NOT NULL
		)`,
		// Harness profile columns (idempotent ALTER — ignored if column exists).
		`ALTER TABLE cards ADD COLUMN harness_profile TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE goals ADD COLUMN harness_profile TEXT NOT NULL DEFAULT ''`,
		// Harness mutation log.
		`CREATE TABLE IF NOT EXISTS harness_mutations (
			id TEXT PRIMARY KEY,
			goal_id TEXT NOT NULL,
			card_id TEXT NOT NULL DEFAULT '',
			iteration INTEGER NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT '',
			old_profile TEXT NOT NULL DEFAULT '{}',
			new_profile TEXT NOT NULL DEFAULT '{}',
			result TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mutations_goal ON harness_mutations(goal_id)`,
		`CREATE TABLE IF NOT EXISTS automation_jobs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			every_seconds INTEGER NOT NULL DEFAULT 30,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_run_at TEXT NOT NULL DEFAULT '',
			last_result TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS webhook_rules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			secret TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL DEFAULT '*',
			agent_id TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS usage_ledger (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_ledger_session ON usage_ledger(session_id)`,
		`CREATE TABLE IF NOT EXISTS mcp_servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			command TEXT NOT NULL DEFAULT '',
			args TEXT NOT NULL DEFAULT '[]',
			env TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'developer',
			system_prompt TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			is_default INTEGER NOT NULL DEFAULT 0,
			is_leader INTEGER NOT NULL DEFAULT 0,
			color TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS session_links (
			id TEXT PRIMARY KEY,
			session_a TEXT NOT NULL,
			session_b TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(session_a, session_b)
		)`,
		`ALTER TABLE agents ADD COLUMN role TEXT NOT NULL DEFAULT 'developer'`,
	}
	// Split: DDL statements run in a transaction; ALTER TABLE statements
	// run individually outside it (SQLite ignores "duplicate column" errors).
	var txStmts, alterStmts []string
	for _, q := range stmts {
		trimmed := strings.TrimSpace(q)
		if strings.HasPrefix(strings.ToUpper(trimmed), "ALTER TABLE") {
			alterStmts = append(alterStmts, q)
		} else {
			txStmts = append(txStmts, q)
		}
	}

	// Transactional DDL (CREATE TABLE / INDEX).
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range txStmts {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w (%s)", err, q[:min(40, len(q))])
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// ALTER TABLE — run outside transaction, ignore "duplicate column" errors.
	for _, q := range alterStmts {
		if _, err := s.db.Exec(q); err != nil {
			// SQLite returns "duplicate column name" for idempotent re-runs.
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migrate alter: %w", err)
			}
		}
	}
	return nil
}

func ts(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTS(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339, s)
		if err2 != nil {
			return time.Time{}
		}
		return t2
	}
	return t
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func unmarshal[T any](raw string, dest *T) {
	if raw == "" {
		return
	}
	_ = json.Unmarshal([]byte(raw), dest)
}

func (s *Store) PutKV(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

func (s *Store) GetKV(ctx context.Context, k string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT v FROM kv WHERE k=?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) UpsertAgent(ctx context.Context, a types.Agent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agents(id,name,profile,model,provider,status,workspace_id,system_prompt,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, profile=excluded.profile, model=excluded.model,
		provider=excluded.provider, status=excluded.status, workspace_id=excluded.workspace_id,
		system_prompt=excluded.system_prompt, updated_at=excluded.updated_at`,
		a.ID, a.Name, a.Profile, a.Model, a.Provider, a.Status, a.WorkspaceID, a.SystemPrompt, ts(a.CreatedAt), ts(a.UpdatedAt))
	return err
}

func (s *Store) GetAgent(ctx context.Context, id types.ID) (types.Agent, error) {
	var a types.Agent
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,profile,model,provider,status,workspace_id,system_prompt,created_at,updated_at FROM agents WHERE id=?`, id).
		Scan(&a.ID, &a.Name, &a.Profile, &a.Model, &a.Provider, &a.Status, &a.WorkspaceID, &a.SystemPrompt, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	a.CreatedAt, a.UpdatedAt = parseTS(created), parseTS(updated)
	return a, err
}

func (s *Store) ListAgents(ctx context.Context) ([]types.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,profile,model,provider,status,workspace_id,system_prompt,created_at,updated_at FROM agents ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Agent
	for rows.Next() {
		var a types.Agent
		var created, updated string
		if err := rows.Scan(&a.ID, &a.Name, &a.Profile, &a.Model, &a.Provider, &a.Status, &a.WorkspaceID, &a.SystemPrompt, &created, &updated); err != nil {
			return nil, err
		}
		a.CreatedAt, a.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAgent(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id=?`, id)
	return err
}

func (s *Store) UpsertSession(ctx context.Context, sess types.Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id,title,agent_id,workspace_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, agent_id=excluded.agent_id, workspace_id=excluded.workspace_id, updated_at=excluded.updated_at`,
		sess.ID, sess.Title, sess.AgentID, sess.WorkspaceID, ts(sess.CreatedAt), ts(sess.UpdatedAt))
	return err
}

func (s *Store) GetSession(ctx context.Context, id types.ID) (types.Session, error) {
	var sess types.Session
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,title,agent_id,workspace_id,created_at,updated_at FROM sessions WHERE id=?`, id).
		Scan(&sess.ID, &sess.Title, &sess.AgentID, &sess.WorkspaceID, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return sess, ErrNotFound
	}
	sess.CreatedAt, sess.UpdatedAt = parseTS(created), parseTS(updated)
	return sess, err
}

func (s *Store) DeleteSession(ctx context.Context, id types.ID) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	return err
}

func (s *Store) DeleteWorkspace(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id=?`, id)
	return err
}

func (s *Store) DeleteProvider(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM providers WHERE id=?`, id)
	return err
}

func (s *Store) ListSessions(ctx context.Context, workspaceID types.ID) ([]types.Session, error) {
	q := `SELECT id,title,agent_id,workspace_id,created_at,updated_at FROM sessions`
	args := []any{}
	if workspaceID != "" {
		q += ` WHERE workspace_id=?`
		args = append(args, workspaceID)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Session
	for rows.Next() {
		var sess types.Session
		var created, updated string
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.AgentID, &sess.WorkspaceID, &created, &updated); err != nil {
			return nil, err
		}
		sess.CreatedAt, sess.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) InsertMessage(ctx context.Context, m types.Message) error {
	var tr any
	if m.ToolResult != nil {
		tr = mustJSON(m.ToolResult)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages(id,session_id,role,content,tool_calls,tool_result,created_at) VALUES(?,?,?,?,?,?,?)`,
		m.ID, m.SessionID, m.Role, m.Content, mustJSON(m.ToolCalls), tr, ts(m.CreatedAt))
	return err
}

func (s *Store) ListMessages(ctx context.Context, sessionID types.ID) ([]types.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,session_id,role,content,tool_calls,tool_result,created_at FROM messages WHERE session_id=? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Message
	for rows.Next() {
		var m types.Message
		var calls, created string
		var tr sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &calls, &tr, &created); err != nil {
			return nil, err
		}
		unmarshal(calls, &m.ToolCalls)
		if tr.Valid && tr.String != "" {
			var r types.ToolResult
			unmarshal(tr.String, &r)
			m.ToolResult = &r
		}
		m.CreatedAt = parseTS(created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) TruncateMessages(ctx context.Context, sessionID types.ID, keep int) error {
	if keep < 0 {
		keep = 0
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE rowid IN (
		SELECT rowid FROM messages WHERE session_id=? ORDER BY created_at LIMIT -1 OFFSET ?
	)`, sessionID, keep)
	return err
}

func (s *Store) UpsertCard(ctx context.Context, c types.Card) error {
	goalMode := 0
	if c.GoalMode {
		goalMode = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO cards(id,title,description,column_name,assignee_agent_id,profile,model,dependencies,acceptance,goal_id,goal_mode,status,terminal_id,git_branch,worktree_path,review_state,artifacts,logs,workspace_id,session_id,lease_holder,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, description=excluded.description, column_name=excluded.column_name,
		assignee_agent_id=excluded.assignee_agent_id, profile=excluded.profile, model=excluded.model, dependencies=excluded.dependencies,
		acceptance=excluded.acceptance, goal_id=excluded.goal_id, goal_mode=excluded.goal_mode, status=excluded.status,
		terminal_id=excluded.terminal_id, git_branch=excluded.git_branch, worktree_path=excluded.worktree_path,
		review_state=excluded.review_state, artifacts=excluded.artifacts, logs=excluded.logs, workspace_id=excluded.workspace_id,
		session_id=excluded.session_id, lease_holder=excluded.lease_holder, updated_at=excluded.updated_at`,
		c.ID, c.Title, c.Description, c.Column, c.AssigneeAgentID, c.Profile, c.Model, mustJSON(c.Dependencies), mustJSON(c.AcceptanceCriteria),
		c.GoalID, goalMode, c.Status, c.TerminalID, c.GitBranch, c.WorktreePath, c.ReviewState, mustJSON(c.Artifacts), mustJSON(c.Logs),
		c.WorkspaceID, c.SessionID, c.LeaseHolder, ts(c.CreatedAt), ts(c.UpdatedAt))
	return err
}

func scanCard(scanner interface{ Scan(dest ...any) error }) (types.Card, error) {
	var c types.Card
	var deps, acc, arts, logs, created, updated string
	var goalMode int
	err := scanner.Scan(&c.ID, &c.Title, &c.Description, &c.Column, &c.AssigneeAgentID, &c.Profile, &c.Model, &deps, &acc, &c.GoalID, &goalMode, &c.Status, &c.TerminalID, &c.GitBranch, &c.WorktreePath, &c.ReviewState, &arts, &logs, &c.WorkspaceID, &c.SessionID, &c.LeaseHolder, &created, &updated)
	if err != nil {
		return c, err
	}
	c.GoalMode = goalMode == 1
	unmarshal(deps, &c.Dependencies)
	unmarshal(acc, &c.AcceptanceCriteria)
	unmarshal(arts, &c.Artifacts)
	unmarshal(logs, &c.Logs)
	c.CreatedAt, c.UpdatedAt = parseTS(created), parseTS(updated)
	return c, nil
}

func (s *Store) GetCard(ctx context.Context, id types.ID) (types.Card, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,title,description,column_name,assignee_agent_id,profile,model,dependencies,acceptance,goal_id,goal_mode,status,terminal_id,git_branch,worktree_path,review_state,artifacts,logs,workspace_id,session_id,lease_holder,created_at,updated_at FROM cards WHERE id=?`, id)
	c, err := scanCard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) ListCards(ctx context.Context, workspaceID types.ID) ([]types.Card, error) {
	q := `SELECT id,title,description,column_name,assignee_agent_id,profile,model,dependencies,acceptance,goal_id,goal_mode,status,terminal_id,git_branch,worktree_path,review_state,artifacts,logs,workspace_id,session_id,lease_holder,created_at,updated_at FROM cards`
	args := []any{}
	if workspaceID != "" {
		q += ` WHERE workspace_id=?`
		args = append(args, workspaceID)
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteCard(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cards WHERE id=?`, id)
	return err
}

func (s *Store) DeleteGoal(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM goals WHERE id=?`, id)
	return err
}

func (s *Store) UpsertGoal(ctx context.Context, g types.Goal) error {
	var verdict any
	if g.LastVerdict != nil {
		verdict = mustJSON(g.LastVerdict)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO goals(id,title,description,contract,status,card_id,workspace_id,agent_id,iteration,last_verdict,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, description=excluded.description, contract=excluded.contract,
		status=excluded.status, card_id=excluded.card_id, workspace_id=excluded.workspace_id, agent_id=excluded.agent_id,
		iteration=excluded.iteration, last_verdict=excluded.last_verdict, updated_at=excluded.updated_at`,
		g.ID, g.Title, g.Description, mustJSON(g.CompletionContract), g.Status, g.CardID, g.WorkspaceID, g.AgentID, g.Iteration, verdict, ts(g.CreatedAt), ts(g.UpdatedAt))
	return err
}

func (s *Store) GetGoal(ctx context.Context, id types.ID) (types.Goal, error) {
	var g types.Goal
	var contract, created, updated string
	var verdict sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,title,description,contract,status,card_id,workspace_id,agent_id,iteration,last_verdict,created_at,updated_at FROM goals WHERE id=?`, id).
		Scan(&g.ID, &g.Title, &g.Description, &contract, &g.Status, &g.CardID, &g.WorkspaceID, &g.AgentID, &g.Iteration, &verdict, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	if err != nil {
		return g, err
	}
	unmarshal(contract, &g.CompletionContract)
	if verdict.Valid && verdict.String != "" {
		var v types.JudgeVerdict
		unmarshal(verdict.String, &v)
		g.LastVerdict = &v
	}
	g.CreatedAt, g.UpdatedAt = parseTS(created), parseTS(updated)
	return g, nil
}

func (s *Store) ListGoals(ctx context.Context) ([]types.Goal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,description,contract,status,card_id,workspace_id,agent_id,iteration,last_verdict,created_at,updated_at FROM goals ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Goal
	for rows.Next() {
		var g types.Goal
		var contract, created, updated string
		var verdict sql.NullString
		if err := rows.Scan(&g.ID, &g.Title, &g.Description, &contract, &g.Status, &g.CardID, &g.WorkspaceID, &g.AgentID, &g.Iteration, &verdict, &created, &updated); err != nil {
			return nil, err
		}
		unmarshal(contract, &g.CompletionContract)
		if verdict.Valid && verdict.String != "" {
			var v types.JudgeVerdict
			unmarshal(verdict.String, &v)
			g.LastVerdict = &v
		}
		g.CreatedAt, g.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) InsertVerdict(ctx context.Context, v types.JudgeVerdict) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO verdicts(id,goal_id,decision,reason,gate_results,iteration,created_at) VALUES(?,?,?,?,?,?,?)`,
		v.ID, v.GoalID, v.Decision, v.Reason, mustJSON(v.GateResults), v.Iteration, ts(v.CreatedAt))
	return err
}

func (s *Store) ListVerdicts(ctx context.Context, goalID types.ID) ([]types.JudgeVerdict, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,goal_id,decision,reason,gate_results,iteration,created_at FROM verdicts WHERE goal_id=? ORDER BY iteration`, goalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.JudgeVerdict
	for rows.Next() {
		var v types.JudgeVerdict
		var gates, created string
		if err := rows.Scan(&v.ID, &v.GoalID, &v.Decision, &v.Reason, &gates, &v.Iteration, &created); err != nil {
			return nil, err
		}
		unmarshal(gates, &v.GateResults)
		v.CreatedAt = parseTS(created)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertWorkspace(ctx context.Context, w types.Workspace) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workspaces(id,name,path,default_branch,created_at,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, path=excluded.path, default_branch=excluded.default_branch, updated_at=excluded.updated_at`,
		w.ID, w.Name, w.Path, w.DefaultBranch, ts(w.CreatedAt), ts(w.UpdatedAt))
	return err
}

func (s *Store) GetWorkspace(ctx context.Context, id types.ID) (types.Workspace, error) {
	var w types.Workspace
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,path,default_branch,created_at,updated_at FROM workspaces WHERE id=?`, id).
		Scan(&w.ID, &w.Name, &w.Path, &w.DefaultBranch, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	w.CreatedAt, w.UpdatedAt = parseTS(created), parseTS(updated)
	return w, err
}

func (s *Store) GetWorkspaceByPath(ctx context.Context, path string) (types.Workspace, error) {
	var w types.Workspace
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,path,default_branch,created_at,updated_at FROM workspaces WHERE path=?`, path).
		Scan(&w.ID, &w.Name, &w.Path, &w.DefaultBranch, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	w.CreatedAt, w.UpdatedAt = parseTS(created), parseTS(updated)
	return w, err
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]types.Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,path,default_branch,created_at,updated_at FROM workspaces ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Workspace
	for rows.Next() {
		var w types.Workspace
		var created, updated string
		if err := rows.Scan(&w.ID, &w.Name, &w.Path, &w.DefaultBranch, &created, &updated); err != nil {
			return nil, err
		}
		w.CreatedAt, w.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) UpsertTerminal(ctx context.Context, t types.TerminalSession) error {
	var ssh any
	if t.SSH != nil {
		ssh = mustJSON(t.SSH)
	}
	pers := 0
	if t.Persistent {
		pers = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO terminals(id,kind,owner_id,workspace_id,title,cwd,shell,cols,rows,pid,status,ssh,persistent,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, owner_id=excluded.owner_id, workspace_id=excluded.workspace_id, title=excluded.title,
		cwd=excluded.cwd, shell=excluded.shell, cols=excluded.cols, rows=excluded.rows, pid=excluded.pid, status=excluded.status,
		ssh=excluded.ssh, persistent=excluded.persistent, updated_at=excluded.updated_at`,
		t.ID, t.Kind, t.OwnerID, t.WorkspaceID, t.Title, t.Cwd, t.Shell, t.Cols, t.Rows, t.PID, t.Status, ssh, pers, ts(t.CreatedAt), ts(t.UpdatedAt))
	return err
}

func (s *Store) GetTerminal(ctx context.Context, id types.ID) (types.TerminalSession, error) {
	var t types.TerminalSession
	var created, updated string
	var ssh sql.NullString
	var pers int
	err := s.db.QueryRowContext(ctx, `SELECT id,kind,owner_id,workspace_id,title,cwd,shell,cols,rows,pid,status,ssh,persistent,created_at,updated_at FROM terminals WHERE id=?`, id).
		Scan(&t.ID, &t.Kind, &t.OwnerID, &t.WorkspaceID, &t.Title, &t.Cwd, &t.Shell, &t.Cols, &t.Rows, &t.PID, &t.Status, &ssh, &pers, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if ssh.Valid && ssh.String != "" {
		var tgt types.SSHTarget
		unmarshal(ssh.String, &tgt)
		t.SSH = &tgt
	}
	t.Persistent = pers == 1
	t.CreatedAt, t.UpdatedAt = parseTS(created), parseTS(updated)
	return t, nil
}

func (s *Store) ListTerminals(ctx context.Context) ([]types.TerminalSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,owner_id,workspace_id,title,cwd,shell,cols,rows,pid,status,ssh,persistent,created_at,updated_at FROM terminals ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.TerminalSession
	for rows.Next() {
		var t types.TerminalSession
		var created, updated string
		var ssh sql.NullString
		var pers int
		if err := rows.Scan(&t.ID, &t.Kind, &t.OwnerID, &t.WorkspaceID, &t.Title, &t.Cwd, &t.Shell, &t.Cols, &t.Rows, &t.PID, &t.Status, &ssh, &pers, &created, &updated); err != nil {
			return nil, err
		}
		if ssh.Valid && ssh.String != "" {
			var tgt types.SSHTarget
			unmarshal(ssh.String, &tgt)
			t.SSH = &tgt
		}
		t.Persistent = pers == 1
		t.CreatedAt, t.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTerminal(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM terminals WHERE id=?`, id)
	return err
}

func (s *Store) UpsertProvider(ctx context.Context, p types.Provider) error {
	def := 0
	if p.Default {
		def = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO providers(id,name,kind,base_url,models,is_default,secret_id) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind, base_url=excluded.base_url, models=excluded.models, is_default=excluded.is_default, secret_id=excluded.secret_id`,
		p.ID, p.Name, p.Kind, p.BaseURL, mustJSON(p.Models), def, p.SecretID)
	return err
}

func (s *Store) ListProviders(ctx context.Context) ([]types.Provider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,base_url,models,is_default,secret_id FROM providers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Provider
	for rows.Next() {
		var p types.Provider
		var models string
		var def int
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &models, &def, &p.SecretID); err != nil {
			return nil, err
		}
		unmarshal(models, &p.Models)
		p.Default = def == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PutMemory(ctx context.Context, e types.MemoryEntry) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO memory(id,scope,scope_id,key,content,created_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(scope,scope_id,key) DO UPDATE SET content=excluded.content, id=excluded.id`,
		e.ID, e.Scope, e.ScopeID, e.Key, e.Content, ts(e.CreatedAt))
	return err
}

func (s *Store) GetMemory(ctx context.Context, scope types.MemoryScope, scopeID types.ID, key string) (types.MemoryEntry, error) {
	var e types.MemoryEntry
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,scope,scope_id,key,content,created_at FROM memory WHERE scope=? AND scope_id=? AND key=?`, scope, scopeID, key).
		Scan(&e.ID, &e.Scope, &e.ScopeID, &e.Key, &e.Content, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	e.CreatedAt = parseTS(created)
	return e, err
}

func (s *Store) ListMemory(ctx context.Context, scope types.MemoryScope, scopeID types.ID) ([]types.MemoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope,scope_id,key,content,created_at FROM memory WHERE scope=? AND scope_id=? ORDER BY created_at`, scope, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.MemoryEntry
	for rows.Next() {
		var e types.MemoryEntry
		var created string
		if err := rows.Scan(&e.ID, &e.Scope, &e.ScopeID, &e.Key, &e.Content, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTS(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMemory(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM memory WHERE id=?`, id)
	return err
}

func (s *Store) UpsertPermission(ctx context.Context, r types.PermissionRule) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO permission_rules(id,action,pattern,decision,skill,agent_id) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET action=excluded.action, pattern=excluded.pattern, decision=excluded.decision, skill=excluded.skill, agent_id=excluded.agent_id`,
		r.ID, r.Action, r.Pattern, r.Decision, r.Skill, r.AgentID)
	return err
}

func (s *Store) ListPermissions(ctx context.Context) ([]types.PermissionRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,action,pattern,decision,skill,agent_id FROM permission_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.PermissionRule
	for rows.Next() {
		var r types.PermissionRule
		if err := rows.Scan(&r.ID, &r.Action, &r.Pattern, &r.Decision, &r.Skill, &r.AgentID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSkill(ctx context.Context, sk types.InstalledSkill) error {
	en := 0
	if sk.Enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO skills(name,manifest,path,source,enabled) VALUES(?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET manifest=excluded.manifest, path=excluded.path, source=excluded.source, enabled=excluded.enabled`,
		sk.Manifest.Name, mustJSON(sk.Manifest), sk.Path, sk.Source, en)
	return err
}

func (s *Store) ListSkills(ctx context.Context) ([]types.InstalledSkill, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name,manifest,path,source,enabled FROM skills ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.InstalledSkill
	for rows.Next() {
		var sk types.InstalledSkill
		var man string
		var en int
		if err := rows.Scan(&sk.Manifest.Name, &man, &sk.Path, &sk.Source, &en); err != nil {
			return nil, err
		}
		unmarshal(man, &sk.Manifest)
		sk.Enabled = en == 1
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSkill(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM skills WHERE name=?`, name)
	return err
}

func (s *Store) AcquireLease(ctx context.Context, path, holder string, cardID types.ID, until time.Time) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var existingHolder, existingExpiry string
	err = tx.QueryRowContext(ctx, `SELECT holder, expires_at FROM file_leases WHERE path=?`, path).Scan(&existingHolder, &existingExpiry)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `INSERT INTO file_leases(path,holder,card_id,expires_at) VALUES(?,?,?,?)`, path, holder, cardID, ts(until))
		if err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		exp := parseTS(existingExpiry)
		if existingHolder != holder && exp.After(now) {
			return fmt.Errorf("%w: %s held by %s until %s", ErrConflict, path, existingHolder, existingExpiry)
		}
		_, err = tx.ExecContext(ctx, `UPDATE file_leases SET holder=?, card_id=?, expires_at=? WHERE path=?`, holder, cardID, ts(until), path)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ReleaseLease(ctx context.Context, path, holder string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM file_leases WHERE path=? AND holder=?`, path, holder)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListLeases(ctx context.Context) ([]types.FileLease, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path,holder,card_id,expires_at FROM file_leases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.FileLease
	for rows.Next() {
		var l types.FileLease
		var exp string
		if err := rows.Scan(&l.Path, &l.Holder, &l.CardID, &exp); err != nil {
			return nil, err
		}
		l.ExpiresAt = parseTS(exp)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) SweepExpiredLeases(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM file_leases WHERE expires_at < ?`, ts(now))
	return err
}

// ── Harness profile persistence ───────────────────────────────────────────────

// SaveGoalHarness persists the harness_profile JSON for a goal.
func (s *Store) SaveGoalHarness(ctx context.Context, goalID types.ID, profileJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE goals SET harness_profile=?, updated_at=? WHERE id=?`,
		profileJSON, ts(time.Now().UTC()), string(goalID))
	return err
}

// LoadGoalHarness returns the raw harness_profile JSON for a goal.
func (s *Store) LoadGoalHarness(ctx context.Context, goalID types.ID) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT harness_profile FROM goals WHERE id=?`, string(goalID)).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return raw, err
}

// SaveCardHarness persists the harness_profile JSON for a card.
func (s *Store) SaveCardHarness(ctx context.Context, cardID types.ID, profileJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE cards SET harness_profile=?, updated_at=? WHERE id=?`,
		profileJSON, ts(time.Now().UTC()), string(cardID))
	return err
}

// LoadCardHarness returns the raw harness_profile JSON for a card.
func (s *Store) LoadCardHarness(ctx context.Context, cardID types.ID) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT harness_profile FROM cards WHERE id=?`, string(cardID)).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return raw, err
}

// InsertHarnessMutation appends a harness mutation record.
func (s *Store) InsertHarnessMutation(ctx context.Context, m HarnessMutationRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO harness_mutations(id,goal_id,card_id,iteration,reason,old_profile,new_profile,result,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		m.ID, m.GoalID, m.CardID, m.Iteration, m.Reason, m.OldProfile, m.NewProfile, m.Result, ts(m.CreatedAt))
	return err
}

// ListHarnessMutations returns all mutations for a goal in order.
func (s *Store) ListHarnessMutations(ctx context.Context, goalID types.ID) ([]HarnessMutationRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,goal_id,card_id,iteration,reason,old_profile,new_profile,result,created_at
		 FROM harness_mutations WHERE goal_id=? ORDER BY created_at ASC`, string(goalID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HarnessMutationRow
	for rows.Next() {
		var m HarnessMutationRow
		var createdAt string
		if err := rows.Scan(&m.ID, &m.GoalID, &m.CardID, &m.Iteration, &m.Reason, &m.OldProfile, &m.NewProfile, &m.Result, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTS(createdAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

// HarnessMutationRow is the store-level representation of a mutation.
type HarnessMutationRow struct {
	ID         string
	GoalID     string
	CardID     string
	Iteration  int
	Reason     string
	OldProfile string // JSON
	NewProfile string // JSON
	Result     string
	CreatedAt  time.Time
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Store) UpsertAutomation(ctx context.Context, j types.AutomationJob) error {
	en := 0
	if j.Enabled {
		en = 1
	}
	last := ""
	if !j.LastRunAt.IsZero() {
		last = ts(j.LastRunAt)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO automation_jobs(id,name,kind,every_seconds,enabled,last_run_at,last_result,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind, every_seconds=excluded.every_seconds,
		enabled=excluded.enabled, last_run_at=excluded.last_run_at, last_result=excluded.last_result, updated_at=excluded.updated_at`,
		j.ID, j.Name, j.Kind, j.EverySeconds, en, last, j.LastResult, ts(j.CreatedAt), ts(j.UpdatedAt))
	return err
}

func (s *Store) ListAutomation(ctx context.Context) ([]types.AutomationJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,every_seconds,enabled,last_run_at,last_result,created_at,updated_at FROM automation_jobs ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.AutomationJob
	for rows.Next() {
		var j types.AutomationJob
		var en int
		var last, created, updated string
		if err := rows.Scan(&j.ID, &j.Name, &j.Kind, &j.EverySeconds, &en, &last, &j.LastResult, &created, &updated); err != nil {
			return nil, err
		}
		j.Enabled = en == 1
		if last != "" {
			j.LastRunAt = parseTS(last)
		}
		j.CreatedAt, j.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAutomation(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM automation_jobs WHERE id=?`, id)
	return err
}

// ── Webhook rules ─────────────────────────────────────────────────────────────

func (s *Store) UpsertWebhookRule(ctx context.Context, r types.WebhookRule) error {
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO webhook_rules(id,name,secret,event_type,agent_id,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, secret=excluded.secret, event_type=excluded.event_type,
		agent_id=excluded.agent_id, enabled=excluded.enabled, updated_at=excluded.updated_at`,
		r.ID, r.Name, r.Secret, r.EventType, r.AgentID, en, ts(r.CreatedAt), ts(r.UpdatedAt))
	return err
}

func (s *Store) ListWebhookRules(ctx context.Context) ([]types.WebhookRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,secret,event_type,agent_id,enabled,created_at,updated_at FROM webhook_rules ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.WebhookRule
	for rows.Next() {
		var r types.WebhookRule
		var en int
		var created, updated string
		if err := rows.Scan(&r.ID, &r.Name, &r.Secret, &r.EventType, &r.AgentID, &en, &created, &updated); err != nil {
			return nil, err
		}
		r.Enabled = en == 1
		r.CreatedAt, r.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteWebhookRule(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM webhook_rules WHERE id=?`, id)
	return err
}

// ── Usage ledger ──────────────────────────────────────────────────────────────

// UsageLedgerEntry is a single cost record.
type UsageLedgerEntry struct {
	ID               string
	SessionID        string
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	CreatedAt        time.Time
}

// RecordUsage inserts a usage ledger entry.
func (s *Store) RecordUsage(ctx context.Context, e UsageLedgerEntry) error {
	if e.ID == "" {
		e.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO usage_ledger(id,session_id,provider,model,prompt_tokens,completion_tokens,cost_usd,created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		e.ID, e.SessionID, e.Provider, e.Model,
		e.PromptTokens, e.CompletionTokens, e.CostUSD, ts(e.CreatedAt))
	return err
}

// SumUsage returns aggregate token counts and total cost.
// If sessionID is non-empty, results are scoped to that session.
func (s *Store) SumUsage(ctx context.Context, sessionID string) (promptTokens, completionTokens int, costUSD float64, err error) {
	q := `SELECT COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cost_usd),0) FROM usage_ledger`
	args := []any{}
	if sessionID != "" {
		q += ` WHERE session_id=?`
		args = append(args, sessionID)
	}
	err = s.db.QueryRowContext(ctx, q, args...).Scan(&promptTokens, &completionTokens, &costUSD)
	return
}

func JoinIDs(ids []types.ID) string {
	ss := make([]string, len(ids))
	for i, id := range ids {
		ss[i] = string(id)
	}
	return strings.Join(ss, ",")
}

// ── MCP server registry ───────────────────────────────────────────────────────

func (s *Store) UpsertMCPServer(ctx context.Context, srv types.MCPServerConfig) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_servers(id,name,command,args,env) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, command=excluded.command, args=excluded.args, env=excluded.env`,
		srv.ID, srv.Name, srv.Command, mustJSON(srv.Args), mustJSON(srv.Env))
	return err
}

func (s *Store) ListMCPServers(ctx context.Context) ([]types.MCPServerConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,command,args,env FROM mcp_servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.MCPServerConfig
	for rows.Next() {
		var srv types.MCPServerConfig
		var args, env string
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Command, &args, &env); err != nil {
			return nil, err
		}
		unmarshal(args, &srv.Args)
		unmarshal(env, &srv.Env)
		out = append(out, srv)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id=?`, id)
	return err
}