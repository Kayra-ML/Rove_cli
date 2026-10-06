package types

import (
	"strings"
	"time"
)

type ID string

func (id ID) String() string { return string(id) }

func (id ID) IsZero() bool { return id == "" }

func NormalizeProviderKind(k ProviderKind) ProviderKind {
	switch strings.ToLower(strings.TrimSpace(string(k))) {
	case "openai", "openai_compat", "openai-compat", "openai-compatible", "openai-compat-v1":
		return ProviderOpenAICompat
	case "anthropic":
		return ProviderAnthropic
	case "fake":
		return ProviderFake
	case "agent-cli", "agent", "cli":
		return ProviderAgentCLI
	default:
		if k == "" {
			return ProviderOpenAICompat
		}
		return k
	}
}

// --- Agent ---

type AgentRole string

const (
	RoleLeader     AgentRole = "leader"
	RoleFrontend   AgentRole = "frontend"
	RoleBackend    AgentRole = "backend"
	RoleDeveloper  AgentRole = "developer"
	RoleDesigner   AgentRole = "designer"
	RoleTester     AgentRole = "tester"
	RoleDebugger   AgentRole = "debugger"
	RoleReviewer   AgentRole = "reviewer"
	RoleResearcher AgentRole = "researcher"
)

// AgentProfile is a reusable configuration template. When IsDefault is true
// it is automatically applied to all newly opened sessions.
type AgentProfile struct {
	ID           ID        `json:"id"`
	Name         string    `json:"name"`
	Role         AgentRole `json:"role"`
	SystemPrompt string    `json:"systemPrompt"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	IsDefault    bool      `json:"isDefault"` // if true, auto-applied to new sessions
	IsLeader     bool      `json:"isLeader"`  // only one leader per workspace
	Color        string    `json:"color,omitempty"`
	// CharacterID picks an expert from the built-in catalog; SystemPrompt
	// then adds to it instead of replacing it. Features lists the enabled
	// feature keys; nil means "the character's defaults".
	CharacterID string   `json:"characterId,omitempty"`
	Features    []string `json:"features,omitempty"`
	// Mark is the agent's logo in the office: "shape:face" (agentmark in
	// the app draws it), tinted with Color. Empty: one picked from its id.
	Mark string `json:"mark,omitempty"`
	// PromptMode says what SystemPrompt is: "" adds it to the character's
	// prompt (or is the whole prompt when there is no character), "own"
	// replaces the character's prompt with it.
	PromptMode string `json:"promptMode,omitempty"`
	// Integrations are the MCP servers (by name) this agent may use; nil
	// lets it use every one, as before integrations were picked per agent.
	Integrations []string `json:"integrations"`
	// Title is the agent's job title in the Agent space ("Senior backend
	// developer"): how colleagues and the user know what to hand it.
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PromptOwn is AgentProfile.PromptMode for a prompt of the user's own.
const PromptOwn = "own"

// SessionPersona is who the agent is inside one session: a catalog
// character or a saved profile, plus feature overrides. It never touches
// the shared agent row, so other sessions are unaffected.
type SessionPersona struct {
	SessionID   ID        `json:"sessionId"`
	CharacterID string    `json:"characterId,omitempty"`
	ProfileID   ID        `json:"profileId,omitempty"`
	Features    []string  `json:"features,omitempty"` // nil = defaults
	ExtraPrompt string    `json:"extraPrompt,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type AgentStatus string

const (
	AgentIdle    AgentStatus = "idle"
	AgentRunning AgentStatus = "running"
	AgentPaused  AgentStatus = "paused"
	AgentFailed  AgentStatus = "failed"
)

type Agent struct {
	ID           ID          `json:"id"`
	Name         string      `json:"name"`
	Role         AgentRole   `json:"role"`
	Profile      string      `json:"profile"`
	Model        string      `json:"model"`
	Provider     string      `json:"provider"`
	Status       AgentStatus `json:"status"`
	WorkspaceID  ID          `json:"workspaceId"`
	SystemPrompt string      `json:"systemPrompt,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

// --- Session / Chat ---

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

type Session struct {
	ID          ID     `json:"id"`
	Title       string `json:"title"`
	AgentID     ID     `json:"agentId"`
	WorkspaceID ID     `json:"workspaceId"`
	// ParentID is set on a team member's channel: a child session that holds
	// one member's own conversation inside a team (orchestra) session. Child
	// sessions stay out of the session list.
	ParentID ID `json:"parentId,omitempty"`
	// Space is the part of the app a chat belongs to: SpaceOffice (talking
	// to an agent) or SpaceChat (session work). Empty on chats made before
	// spaces existed; clients place those themselves.
	Space     string    `json:"space,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const (
	SpaceOffice = "office"
	SpaceChat   = "chat"
	// SpaceTeamwork marks the channels a Teamwork plan opens for its agents
	// (child sessions of the chat); they are not Orchestra team members.
	SpaceTeamwork = "teamwork"
	// SpaceTerminal marks a terminal of a multi-terminal session: a pane of
	// the terminal mode, a child of the session it belongs to.
	SpaceTerminal = "terminal"
	// SpaceWorker marks a subagent the lead spun up for one task: a child of
	// the chat with no persona and no standing on the team. It is kept so
	// its transcript can be read, and shown only in the subagent roster.
	SpaceWorker = "worker"
	// SpaceMap marks a chat's context-map assistant: a child of the chat you
	// ask about its context from the map. It is never a team member or pane
	// and is shown only on the map.
	SpaceMap = "map"
	// SpaceOrchestra marks a member of one Teamwork part's orchestra: a
	// child of the part's conductor channel (itself a SpaceTeamwork child of
	// the chat). The conductor hands it work with team_delegate.
	SpaceOrchestra = "orchestra"
)

// SessionLink connects two sessions so they can relay messages to each other.
// It is a cable on the context map.
type SessionLink struct {
	ID        ID     `json:"id"`
	SessionA  ID     `json:"sessionA"`
	SessionB  ID     `json:"sessionB"`
	Label     string `json:"label,omitempty"`
	Direction string `json:"direction"` // LinkBoth, LinkAToB or LinkBToA
	Auto      bool   `json:"auto"`      // relay file changes without being asked
	Mode      string `json:"mode"`      // LinkSmart or LinkAlways
	// Color is the cable's colour on the map, one of CableColors; empty is
	// the default.
	Color     string    `json:"color,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// CableColors are the colours a map cable may take: the categorical
// palette's hues, each drawn in the step that suits the theme.
var CableColors = []string{"blue", "orange", "aqua", "yellow", "magenta", "green", "violet", "red"}

// ValidCableColor reports whether c is a cable colour (empty included).
func ValidCableColor(c string) bool {
	if c == "" {
		return true
	}
	for _, x := range CableColors {
		if x == c {
			return true
		}
	}
	return false
}

const (
	LinkBoth = "both"
	LinkAToB = "a2b"
	LinkBToA = "b2a"

	// LinkSmart runs the far agent only when a zero-token check finds a
	// counterpart of the change in its workspace. LinkAlways always runs it.
	LinkSmart  = "smart"
	LinkAlways = "always"
)

// Flows reports whether changes in session from travel to session to.
func (l SessionLink) Flows(from, to ID) bool {
	switch {
	case l.SessionA == from && l.SessionB == to:
		return l.Direction != LinkBToA
	case l.SessionB == from && l.SessionA == to:
		return l.Direction != LinkAToB
	}
	return false
}

// Other returns the session on the far end of the link from id.
func (l SessionLink) Other(id ID) ID {
	if l.SessionA == id {
		return l.SessionB
	}
	return l.SessionA
}

// MapNode is a session placed on the context map canvas.
type MapNode struct {
	SessionID ID        `json:"sessionId"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Relay is one message carried along a link: an automatic change notice or
// a manual message. Status: queued, running, done, skipped, failed.
type Relay struct {
	ID      ID       `json:"id"`
	LinkID  ID       `json:"linkId,omitempty"`
	From    ID       `json:"from"`
	To      ID       `json:"to"`
	Kind    string   `json:"kind"` // auto | manual | agent
	Status  string   `json:"status"`
	Hop     int      `json:"hop"`
	Files   []string `json:"files,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Error   string   `json:"error,omitempty"`
	// Matches are the target files the pre-check found; Tokens estimates the
	// notice size (0 when the relay was skipped without calling a model).
	Matches   []string  `json:"matches,omitempty"`
	Tokens    int       `json:"tokens"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ArgsJSON string `json:"argsJson"`
}

type ToolResult struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	IsError    bool   `json:"isError"`
	// Kind tells a refusal from a failure: ToolRefused when a rule or the
	// session's tool policy would not let the call run, "" otherwise. The
	// model treats both as failed; the app shows them differently, since a
	// refusal is a decision and not something that went wrong.
	Kind string `json:"kind,omitempty"`
}

// ToolRefused is the Kind of a tool result the app must not show as an error.
const ToolRefused = "refused"

type Message struct {
	ID         ID          `json:"id"`
	SessionID  ID          `json:"sessionId"`
	Role       MessageRole `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"toolCalls,omitempty"`
	ToolResult *ToolResult `json:"toolResult,omitempty"`
	// Images are data: URLs attached to a user message.
	Images []string `json:"images,omitempty"`
	// Kind marks special messages: "summary" is a compaction summary that
	// stands for every message before it.
	Kind      string    `json:"kind,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// MessageSummary is the Kind of a compaction summary.
const MessageSummary = "summary"

// TurnEdit is a file a run changed, with what it held before, so the change
// can be reviewed and taken back.
type TurnEdit struct {
	RunID     ID        `json:"runId"`
	SessionID ID        `json:"sessionId"`
	Workspace string    `json:"workspace"`
	Path      string    `json:"path"` // relative to Workspace
	Before    string    `json:"-"`
	Existed   bool      `json:"existed"`
	Status    string    `json:"status"` // pending | accepted | reverted
	CreatedAt time.Time `json:"createdAt"`
}

// --- Goals / Judge ---

type Artifact struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Path  string `json:"path"`
	URL   string `json:"url,omitempty"`
	Kind  string `json:"kind"`
}

type GoalStatus string

const (
	GoalPending GoalStatus = "pending"
	GoalRunning GoalStatus = "running"
	GoalDone    GoalStatus = "done"
	GoalBlocked GoalStatus = "blocked"
	GoalFailed  GoalStatus = "failed"
	// GoalCanceled: stopped by the user (/stop or goal.cancel); it can be
	// resumed from its checkpoint.
	GoalCanceled GoalStatus = "canceled"
)

type QualityGateKind string

const (
	GateBuild  QualityGateKind = "build"
	GateTest   QualityGateKind = "test"
	GateLint   QualityGateKind = "lint"
	GateCustom QualityGateKind = "custom"
)

type QualityGate struct {
	Name           string          `json:"name"`
	Kind           QualityGateKind `json:"kind"`
	Command        string          `json:"command"`
	WorkDir        string          `json:"workDir,omitempty"`
	ExpectExitZero bool            `json:"expectExitZero"`
	TimeoutSeconds int             `json:"timeoutSeconds,omitempty"`
}

type CompletionContract struct {
	Criteria      []string      `json:"criteria"`
	QualityGates  []QualityGate `json:"qualityGates,omitempty"`
	MaxIterations int           `json:"maxIterations"`
}

type GateResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

type JudgeDecision string

const (
	JudgeDONE     JudgeDecision = "DONE"
	JudgeCONTINUE JudgeDecision = "CONTINUE"
	JudgeBLOCKED  JudgeDecision = "BLOCKED"
)

type JudgeVerdict struct {
	ID          ID            `json:"id"`
	GoalID      ID            `json:"goalId"`
	Decision    JudgeDecision `json:"decision"`
	Reason      string        `json:"reason"`
	GateResults []GateResult  `json:"gateResults,omitempty"`
	Iteration   int           `json:"iteration"`
	CreatedAt   time.Time     `json:"createdAt"`
}

type Goal struct {
	ID                 ID                 `json:"id"`
	Title              string             `json:"title"`
	Description        string             `json:"description"`
	CompletionContract CompletionContract `json:"completionContract"`
	Status             GoalStatus         `json:"status"`
	WorkspaceID        ID                 `json:"workspaceId"`
	AgentID            ID                 `json:"agentId"`
	// SessionID is the chat the goal works in (empty for a goal of its own).
	SessionID   ID            `json:"sessionId,omitempty"`
	Iteration   int           `json:"iteration"`
	LastVerdict *JudgeVerdict `json:"lastVerdict,omitempty"`
	// Harness execution policy — persisted with the goal.
	HarnessProfileJSON string    `json:"-"` // raw JSON for store
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// --- Skills ---

type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

type SkillPermissions struct {
	Filesystem bool     `json:"filesystem"`
	Shell      bool     `json:"shell"`
	Network    bool     `json:"network"`
	Browser    bool     `json:"browser"`
	Git        bool     `json:"git"`
	Dangerous  []string `json:"dangerous,omitempty"`
}

func (p SkillPermissions) DangerousCapabilities() []string {
	var out []string
	if p.Filesystem {
		out = append(out, "filesystem")
	}
	if p.Shell {
		out = append(out, "shell")
	}
	if p.Network {
		out = append(out, "network")
	}
	if p.Browser {
		out = append(out, "browser")
	}
	if p.Git {
		out = append(out, "git")
	}
	out = append(out, p.Dangerous...)
	return out
}

type SkillManifest struct {
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Author        string            `json:"author"`
	Description   string            `json:"description"`
	Entrypoints   []string          `json:"entrypoints,omitempty"`
	RequiredTools []string          `json:"requiredTools,omitempty"`
	Hooks         map[string]string `json:"hooks,omitempty"`
	Commands      []SlashCommand    `json:"commands,omitempty"`
	Dependencies  []string          `json:"dependencies,omitempty"`
	Permissions   SkillPermissions  `json:"permissions"`
	Homepage      string            `json:"homepage,omitempty"`
	License       string            `json:"license,omitempty"`
	Compatibility map[string]string `json:"compatibility,omitempty"`
}

type InstalledSkill struct {
	Manifest SkillManifest `json:"manifest"`
	Path     string        `json:"path"`
	Source   string        `json:"source"`
	Enabled  bool          `json:"enabled"`
}

type MarketSkill struct {
	Manifest SkillManifest `json:"manifest"`
	Source   string        `json:"source"`
	Rating   float64       `json:"rating,omitempty"`
	Signed   bool          `json:"signed"`
	Checksum string        `json:"checksum,omitempty"`
	Kind     string        `json:"kind,omitempty"`   // "plugin" | "skill"
	Plugin   string        `json:"plugin,omitempty"` // parent plugin id for skills
}

// --- Terminal ---

type TerminalKind string

const (
	TermUser  TerminalKind = "user"
	TermAgent TerminalKind = "agent"
)

type TerminalStatus string

const (
	TermRunning  TerminalStatus = "running"
	TermExited   TerminalStatus = "exited"
	TermDetached TerminalStatus = "detached"
)

type SSHTarget struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	AuthMethod string `json:"authMethod"` // key, password, agent
	KeyPath    string `json:"keyPath,omitempty"`
}

type TerminalSession struct {
	ID          ID             `json:"id"`
	Kind        TerminalKind   `json:"kind"`
	OwnerID     ID             `json:"ownerId"`
	WorkspaceID ID             `json:"workspaceId"`
	Title       string         `json:"title"`
	Cwd         string         `json:"cwd"`
	Shell       string         `json:"shell"`
	Cols        int            `json:"cols"`
	Rows        int            `json:"rows"`
	PID         int            `json:"pid"`
	Status      TerminalStatus `json:"status"`
	SSH         *SSHTarget     `json:"ssh,omitempty"`
	Persistent  bool           `json:"persistent"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// --- Provider ---

type ProviderKind string

const (
	ProviderOpenAICompat ProviderKind = "openai-compat"
	ProviderAnthropic    ProviderKind = "anthropic"
	ProviderFake         ProviderKind = "fake"
	// ProviderAgentCLI is an agent system on this computer (Claude Code,
	// Codex, Antigravity, Hermes) signed in with the user's own account; its
	// base URL is agent://<system>?access=edits|full.
	ProviderAgentCLI ProviderKind = "agent-cli"
)

type Provider struct {
	ID       ID           `json:"id"`
	Name     string       `json:"name"`
	Kind     ProviderKind `json:"kind"`
	BaseURL  string       `json:"baseUrl"`
	Models   []string     `json:"models"`
	Default  bool         `json:"default"`
	SecretID string       `json:"secretId,omitempty"`
}

type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// --- Workspace / Git ---

type Workspace struct {
	ID            ID        `json:"id"`
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	DefaultBranch string    `json:"defaultBranch"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Worktree struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Head     string `json:"head,omitempty"`
	Detached bool   `json:"detached"`
}

type GitStatus struct {
	Branch    string   `json:"branch"`
	Dirty     bool     `json:"dirty"`
	Ahead     int      `json:"ahead"`
	Behind    int      `json:"behind"`
	Changed   []string `json:"changed,omitempty"`
	Untracked []string `json:"untracked,omitempty"`
}

// --- Memory ---

type MemoryScope string

const (
	MemGlobal    MemoryScope = "global"
	MemWorkspace MemoryScope = "workspace"
	MemSession   MemoryScope = "session"
	MemAgent     MemoryScope = "agent"
	// MemProfile is an Office agent's own notes (scope id: its profile),
	// carried into every conversation it has.
	MemProfile MemoryScope = "profile"
)

type MemoryEntry struct {
	ID        ID          `json:"id"`
	Scope     MemoryScope `json:"scope"`
	ScopeID   ID          `json:"scopeId,omitempty"`
	Key       string      `json:"key"`
	Content   string      `json:"content"`
	CreatedAt time.Time   `json:"createdAt"`
}

// --- Permissions ---

type PermissionAction string

const (
	PermFilesystem PermissionAction = "filesystem"
	PermShell      PermissionAction = "shell"
	PermNetwork    PermissionAction = "network"
	PermBrowser    PermissionAction = "browser"
	PermGit        PermissionAction = "git"
	PermSecrets    PermissionAction = "secrets"
)

type PermissionDecision string

const (
	PermAllow PermissionDecision = "allow"
	PermDeny  PermissionDecision = "deny"
	PermAsk   PermissionDecision = "ask"
)

type PermissionRule struct {
	ID       ID                 `json:"id"`
	Action   PermissionAction   `json:"action"`
	Pattern  string             `json:"pattern"`
	Decision PermissionDecision `json:"decision"`
	Skill    string             `json:"skill,omitempty"`
	AgentID  ID                 `json:"agentId,omitempty"`
}

// --- Events ---

type EventType string

const (
	EventMessageDelta   EventType = "message.delta"
	EventMessageDone    EventType = "message.done"
	EventToolStart      EventType = "tool.start"
	EventToolResult     EventType = "tool.result"
	EventSessionUpdated EventType = "session.updated"
	EventGoalUpdated    EventType = "goal.updated"
	EventJudgeVerdict   EventType = "judge.verdict"
	EventTerminalData   EventType = "terminal.data"
	EventTerminalExit   EventType = "terminal.exit"
	EventAgentStatus    EventType = "agent.status"
	EventRunDone        EventType = "run.done"
	EventLog            EventType = "log"
	EventError          EventType = "error"
	EventAutomation     EventType = "automation.tick"
	EventRelay          EventType = "ctxmap.relay"
	EventMapChanged     EventType = "ctxmap.changed"
)

type Event struct {
	Type      EventType      `json:"type"`
	Topic     string         `json:"topic,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// --- Webhook ---

type WebhookRule struct {
	ID        ID        `json:"id"`
	Name      string    `json:"name"`
	Secret    string    `json:"secret"`
	EventType string    `json:"eventType"`
	AgentID   ID        `json:"agentId"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// --- MCP servers ---

var mcpFold = map[rune]rune{'ş': 's', 'ç': 'c', 'ğ': 'g', 'ı': 'i', 'ö': 'o', 'ü': 'u', 'â': 'a', 'î': 'i', 'û': 'u', 'é': 'e', 'è': 'e', 'á': 'a', 'à': 'a', 'ñ': 'n', 'ä': 'a', 'ß': 's'}

// MCPSlug is a server's name as it appears in its tools' names
// ("mcp_<slug>_<tool>"): lower case letters, digits and dashes, so the
// names pass every provider's tool-name rules and one server's prefix is
// never another's.
func MCPSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if a, ok := mcpFold[r]; ok {
			r = a
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 24 {
		out = strings.TrimRight(out[:24], "-")
	}
	if out == "" {
		out = "server"
	}
	return out
}

type MCPServerConfig struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// --- Automation ---

type AutomationKind string

const (
	AutoDriveGoals AutomationKind = "drive_goals"
)

type AutomationJob struct {
	ID           ID             `json:"id"`
	Name         string         `json:"name"`
	Kind         AutomationKind `json:"kind"`
	EverySeconds int            `json:"everySeconds"`
	Enabled      bool           `json:"enabled"`
	LastRunAt    time.Time      `json:"lastRunAt,omitempty"`
	LastResult   string         `json:"lastResult,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

// --- Automation Template (catalog entry) ---

// AutomationTemplate is a read-only blueprint that lives in the bundled
// automations/ directory.  Installing one creates an AutomationJob record.
type AutomationTemplate struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Author       string   `json:"author"`
	Description  string   `json:"description"`
	Kind         string   `json:"kind"`
	Tags         []string `json:"tags,omitempty"`
	EverySeconds int      `json:"everySeconds"`
	Permissions  struct {
		Shell   bool `json:"shell"`
		Network bool `json:"network"`
		Git     bool `json:"git"`
	} `json:"permissions"`
	// Installed is true when an AutomationJob with matching Name already exists.
	Installed bool `json:"installed"`
}
