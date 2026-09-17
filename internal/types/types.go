package types

import "time"

type ID string

func (id ID) String() string { return string(id) }

func (id ID) IsZero() bool { return id == "" }

// --- Agent ---

type AgentStatus string

const (
	AgentIdle    AgentStatus = "idle"
	AgentRunning AgentStatus = "running"
	AgentPaused  AgentStatus = "paused"
	AgentFailed  AgentStatus = "failed"
)

type Agent struct {
	ID          ID          `json:"id"`
	Name        string      `json:"name"`
	Profile     string      `json:"profile"`
	Model       string      `json:"model"`
	Provider    string      `json:"provider"`
	Status      AgentStatus `json:"status"`
	WorkspaceID ID          `json:"workspaceId"`
	SystemPrompt string     `json:"systemPrompt,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
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
	ID          ID        `json:"id"`
	Title       string    `json:"title"`
	AgentID     ID        `json:"agentId"`
	WorkspaceID ID        `json:"workspaceId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
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
}

type Message struct {
	ID         ID          `json:"id"`
	SessionID  ID          `json:"sessionId"`
	Role       MessageRole `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"toolCalls,omitempty"`
	ToolResult *ToolResult `json:"toolResult,omitempty"`
	CreatedAt  time.Time   `json:"createdAt"`
}

// --- Kanban ---

type KanbanColumn string

const (
	ColBacklog KanbanColumn = "backlog"
	ColReady   KanbanColumn = "ready"
	ColRunning KanbanColumn = "running"
	ColReview  KanbanColumn = "review"
	ColDone    KanbanColumn = "done"
	ColBlocked KanbanColumn = "blocked"
)

func ValidColumn(c KanbanColumn) bool {
	switch c {
	case ColBacklog, ColReady, ColRunning, ColReview, ColDone, ColBlocked:
		return true
	}
	return false
}

type ReviewState string

const (
	ReviewNone     ReviewState = ""
	ReviewPending  ReviewState = "pending"
	ReviewApproved ReviewState = "approved"
	ReviewRejected ReviewState = "rejected"
)

type Artifact struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Path  string `json:"path"`
	URL   string `json:"url,omitempty"`
	Kind  string `json:"kind"`
}

type LogEntry struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Source  string    `json:"source,omitempty"`
}

type Card struct {
	ID                 ID            `json:"id"`
	Title              string        `json:"title"`
	Description        string        `json:"description"`
	Column             KanbanColumn  `json:"column"`
	AssigneeAgentID    ID            `json:"assigneeAgentId,omitempty"`
	Profile            string        `json:"profile"`
	Model              string        `json:"model"`
	Dependencies       []ID          `json:"dependencies,omitempty"`
	AcceptanceCriteria []string      `json:"acceptanceCriteria,omitempty"`
	GoalID             ID            `json:"goalId,omitempty"`
	GoalMode           bool          `json:"goalMode"`
	Status             string        `json:"status"`
	TerminalID         ID            `json:"terminalId,omitempty"`
	GitBranch          string        `json:"gitBranch,omitempty"`
	WorktreePath       string        `json:"worktreePath,omitempty"`
	ReviewState        ReviewState   `json:"reviewState,omitempty"`
	Artifacts          []Artifact    `json:"artifacts,omitempty"`
	Logs               []LogEntry    `json:"logs,omitempty"`
	WorkspaceID        ID            `json:"workspaceId"`
	SessionID          ID            `json:"sessionId,omitempty"`
	LeaseHolder        string        `json:"leaseHolder,omitempty"`
	// HarnessProfileJSON stores the JSON-serialised harness.HarnessProfile.
	HarnessProfileJSON string        `json:"-"`
	CreatedAt          time.Time     `json:"createdAt"`
	UpdatedAt          time.Time     `json:"updatedAt"`
}

// --- Goals / Judge ---

type GoalStatus string

const (
	GoalPending GoalStatus = "pending"
	GoalRunning GoalStatus = "running"
	GoalDone    GoalStatus = "done"
	GoalBlocked GoalStatus = "blocked"
	GoalFailed  GoalStatus = "failed"
)

type QualityGateKind string

const (
	GateBuild  QualityGateKind = "build"
	GateTest   QualityGateKind = "test"
	GateLint   QualityGateKind = "lint"
	GateCustom QualityGateKind = "custom"
)

type QualityGate struct {
	Name            string          `json:"name"`
	Kind            QualityGateKind `json:"kind"`
	Command         string          `json:"command"`
	WorkDir         string          `json:"workDir,omitempty"`
	ExpectExitZero  bool            `json:"expectExitZero"`
	TimeoutSeconds  int             `json:"timeoutSeconds,omitempty"`
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
	ID                  ID                 `json:"id"`
	Title               string             `json:"title"`
	Description         string             `json:"description"`
	CompletionContract  CompletionContract `json:"completionContract"`
	Status              GoalStatus         `json:"status"`
	CardID              ID                 `json:"cardId"`
	WorkspaceID         ID                 `json:"workspaceId"`
	AgentID             ID                 `json:"agentId"`
	Iteration           int                `json:"iteration"`
	LastVerdict         *JudgeVerdict      `json:"lastVerdict,omitempty"`
	// Harness execution policy — persisted with the goal.
	HarnessProfileJSON  string             `json:"-"` // raw JSON for store
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
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
	CardID   ID     `json:"cardId,omitempty"`
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
	EventCardUpdated    EventType = "card.updated"
	EventCardMoved      EventType = "card.moved"
	EventGoalUpdated    EventType = "goal.updated"
	EventJudgeVerdict   EventType = "judge.verdict"
	EventTerminalData   EventType = "terminal.data"
	EventTerminalExit   EventType = "terminal.exit"
	EventAgentStatus    EventType = "agent.status"
	EventLog            EventType = "log"
	EventError          EventType = "error"
	EventAutomation     EventType = "automation.tick"
)

type Event struct {
	Type      EventType      `json:"type"`
	Topic     string         `json:"topic,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// --- File coordination ---

type FileLease struct {
	Path      string    `json:"path"`
	Holder    string    `json:"holder"`
	CardID    ID        `json:"cardId,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// --- Automation ---

type AutomationKind string

const (
	AutoSweepReady AutomationKind = "sweep_ready"
	AutoAssignIdle AutomationKind = "assign_idle"
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

// --- MCP server ---

type MCPServerConfig struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// --- Webhook ---

// WebhookRule defines an incoming HTTP webhook trigger that fires an agent.
type WebhookRule struct {
	ID        ID        `json:"id"`
	Name      string    `json:"name"`
	Secret    string    `json:"secret,omitempty"`
	EventType string    `json:"eventType"` // "*" matches all events
	AgentID   ID        `json:"agentId"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}


