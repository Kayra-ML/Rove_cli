export type ID = string;

export type Agent = {
  id: ID;
  name: string;
  profile: string;
  model: string;
  provider: string;
  status: string;
  workspaceId: ID;
  systemPrompt?: string;
};

export type Session = {
  id: ID;
  title: string;
  agentId: ID;
  workspaceId: ID;
  // set on a channel: a subagent's, a terminal's, a Teamwork player's
  parentId?: ID;
  // "office" or "chat"; empty on chats made before spaces existed. "worker"
  // is a one-off subagent's channel, opened from the subagent roster.
  space?: "office" | "chat" | "worker";
  createdAt?: string;
  updatedAt: string;
};

// A piece of work a chat handed to a subagent (team_delegate), as the
// daemon's roster reports it. See internal/team/subagents.go.
export type SubagentStatus = "queued" | "running" | "completed" | "failed" | "interrupted" | "max_turns";
export type SubagentTask = {
  id: string;
  batch: string;
  index: number;
  of: number;
  parentId: ID;
  sessionId: ID;
  goal: string;
  status: SubagentStatus;
  startedAt?: string;
  endedAt?: string;
  turns?: number;
  summary?: string;
  error?: string;
  files?: string[];
  // its own checkout, when it ran in one, and whether its work landed
  worktree?: {
    repo: string;
    path: string;
    branch: string;
    base: string;
    added: number;
    removed: number;
    files?: string[];
    applied: boolean;
    // why it did not land: words for the lead, and a code for the app
    pending?: string;
    reason?: "clash" | "unfinished" | "unreadable" | "discarded";
  };
};

export type ToolCall = { id: string; name: string; argsJson?: string };
// kind "refused" is a call a rule would not let run: failed for the model,
// but a decision and not a fault, so the app does not show it as an error.
export type ToolResult = { toolCallId: string; name: string; content: string; isError?: boolean; kind?: string };

export type Message = {
  id: ID;
  sessionId: ID;
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  toolCalls?: ToolCall[] | null;
  toolResult?: ToolResult | null;
  // images sent with a user message (data: URLs)
  images?: string[] | null;
  // "summary": a compaction summary standing for the messages before it
  kind?: string;
  createdAt: string;
};

export type Workspace = { id: ID; name: string; path: string; defaultBranch: string };
export type Provider = { id: ID; name: string; kind: string; baseUrl: string; models: string[]; default: boolean };
export type InstalledSkill = {
  manifest: {
    name: string;
    version: string;
    author: string;
    description: string;
    permissions: { filesystem?: boolean; shell?: boolean; network?: boolean; browser?: boolean; git?: boolean };
    commands?: { name: string; description: string }[];
  };
  path: string;
  source: string;
  enabled: boolean;
};
export type MarketSkill = {
  manifest: { name: string; version: string; author: string; description: string };
  source: string;
  rating?: number;
  signed: boolean;
  checksum?: string;
  versions?: string[];
  kind?: "plugin" | "skill" | string;
  plugin?: string;
};
export type SSHTarget = {
  // an address, or a Host alias of ~/.ssh/config (ssh then applies the
  // config's HostName, User, Port, keys…)
  host: string;
  // the address an alias points to, for display
  hostName?: string;
  port?: number;
  user?: string;
  authMethod?: "key" | "password" | "agent" | string;
  keyPath?: string;
};

export type TerminalSession = {
  id: ID;
  kind: string;
  title: string;
  cwd: string;
  cols: number;
  rows: number;
  status: string;
  pid: number;
  ssh?: SSHTarget;
};
export type Goal = {
  id: ID;
  title: string;
  status: string;
  iteration: number;
  workspaceId?: ID;
  agentId?: ID;
};

export type AutomationJob = {
  id: ID;
  name: string;
  kind: "drive_goals" | string;
  everySeconds: number;
  enabled: boolean;
  lastRunAt?: string;
  lastResult?: string;
};

export type AutomationTemplate = {
  name: string;
  version: string;
  author: string;
  description: string;
  kind: string;
  tags: string[];
  everySeconds: number;
  permissions: { shell: boolean; network: boolean; git: boolean };
  installed: boolean;
};

export type AgentRole = "leader" | "frontend" | "backend" | "developer" | "designer" | "tester" | "debugger" | "reviewer" | "researcher";

export type AgentProfile = {
  id: ID;
  name: string;
  role: AgentRole;
  systemPrompt: string;
  model: string;
  provider: string;
  isDefault: boolean;
  isLeader: boolean;
  color?: string;
  characterId?: string;
  features?: string[] | null;
  // the office logo: "shape:face", tinted with color (see lib/agentmark)
  mark?: string;
  // "own": systemPrompt replaces the character's prompt; empty adds to it
  promptMode?: "" | "own";
  // the agent's job title in the Agent space ("Senior backend developer")
  title?: string;
  // the MCP servers (by name) it may use; null: every one
  integrations?: string[] | null;
  createdAt?: string;
  updatedAt?: string;
};

// The Agent space's team (see internal/staff): a task handed to an agent,
// by the user or by a colleague, and what each agent is up to.
export type StaffTaskStatus = "running" | "done" | "failed" | "stopped";
export type StaffTask = {
  id: ID;
  profileId: ID;
  profileName?: string;
  sessionId: ID;
  title: string;
  brief: string;
  // the colleague who asked; empty when the user did
  from?: ID;
  fromName?: string;
  // a Session Map watch set it off, watching this chat
  watchId?: ID;
  scheduleId?: ID;
  handoffId?: ID;
  origin?: string;
  status: StaffTaskStatus;
  report?: string;
  error?: string;
  seen: boolean;
  createdAt: string;
  endedAt?: string;
  // the task's own checkout and what became of its work (internal/staff/isolate.go)
  isolation?: StaffIsolation;
};
export type StaffIsolation = {
  state: "working" | "review" | "applied" | "discarded" | "pr" | "empty" | "carried";
  files?: string[];
  added: number;
  removed: number;
  pr?: string;
  note?: string;
};
export type StaffNote = { id: ID; key: string; content: string; createdAt: string };
// automations handing an agent work: a chat it watches, a timer
export type StaffWatch = { id: ID; sessionId: ID; profileId: ID; instruction: string; filter?: string; dailyLimit: number; enabled: boolean; color?: string };
export type StaffHandoff = { id: ID; fromId: ID; toId: ID; instruction: string; dailyLimit: number; enabled: boolean; color?: string };
export type StaffMonitor = { id: ID; profileId: ID; name: string; command: string; instruction: string; everyMinutes: number; workspaceId?: ID; enabled: boolean; lastRun?: string; lastError?: string };
export type MonitorPreset = { key: string; name: string; command: string; instruction: string; every: number; needs: string };
export type StaffSchedule = { id: ID; profileId: ID; instruction: string; everyMinutes?: number; dailyAt?: string; workspaceId?: ID; enabled: boolean; lastRun?: string };
export type StaffMember = {
  profile: AgentProfile;
  state: "working" | "report" | "idle";
  now?: string;
  nowSession?: ID;
  tasks: StaffTask[];
  notes: StaffNote[];
};

export type Character = {
  id: string;
  name: string;
  category: string;
  summary: string;
  tags?: string[];
  role: AgentRole;
  features: string[];
  prompt: string;
  // name/summary/category in the other app languages (Turkish is the base)
  i18n?: Record<string, CatalogText>;
};

export type CatalogText = { name: string; summary?: string; category?: string };

export type Feature = {
  key: string;
  group: "tools" | "behavior";
  name: string;
  desc: string;
  tools?: string[];
  prefix?: string;
  rule?: string;
  // name/desc (as name/summary) in the other app languages
  i18n?: Record<string, CatalogText>;
};

export type PersonaCatalog = { characters: Character[]; features: Feature[]; defaults: string[] };

export type Persona = {
  sessionId: ID;
  source: "none" | "character" | "profile" | "default-profile" | "features";
  characterId?: string;
  profileId?: ID;
  name?: string;
  features: string[];
  extraPrompt?: string;
  prompt?: string;
  model?: string;
};

export type PersonaView = { persona: Persona; cost: { prompt: number; tools: number; total: number; toolsOff: number } };

export type PersonaBadge = { name: string; characterId?: string; profileId?: string; mark?: string; color?: string };

export type SessionLink = {
  id: ID;
  sessionA: ID;
  sessionB: ID;
  label?: string;
  direction?: "both" | "a2b" | "b2a";
  auto?: boolean;
  mode?: "smart" | "always";
  // the cable's colour on the map, one of CABLE_COLORS; empty is the default
  color?: string;
  createdAt?: string;
};

export type CtxNode = { sessionId: ID; x: number; y: number };

export type Relay = {
  id: ID;
  linkId?: ID;
  from: ID;
  to: ID;
  kind: "auto" | "manual" | "agent" | string;
  status: "queued" | "waiting" | "running" | "done" | "skipped" | "failed" | string;
  hop: number;
  files?: string[];
  matches?: string[];
  tokens: number;
  summary?: string;
  error?: string;
  createdAt?: string;
};

export type RpcResponse<T> = { id?: string; ok: boolean; result?: T; error?: string };

// ── Project map (codemap.*) ────────────────────────────────────────────────

export type MapNode = {
  id: string;
  label: string;
  dir: string;
  group: string;
  lang: string;
  loc: number;
  rank: number;
  in: number;
  out: number;
  symbols: number;
};

export type MapEdge = { from: string; to: string; kind: "import" | "call" | string };

export type MapGraph = {
  root: string;
  nodes: MapNode[];
  edges: MapEdge[];
  total: number;
  totalEdges: number;
  truncated: boolean;
};

export type MapStatus = {
  root: string;
  files: number;
  edges: number;
  symbols: number;
  updatedAt: string;
  scanMs: number;
  truncated: boolean;
  langs: Record<string, number>;
};

export type MapSymbol = { name: string; kind: string; line: number; refs?: number };

export type MapNeighbors = {
  file: string;
  lang: string;
  loc: number;
  symbols: MapSymbol[] | null;
  exports?: string[];
  importedBy?: string[];
  imports?: string[];
  calledBy?: { symbol: string; file: string; caller: string; line: number }[];
};

export type MapHit = { file: string; lang: string; loc: number; score: number; why?: string[]; symbols?: string[] };

// Teamwork: a planned, phased team effort in a chat (internal/teamwork).
export type WorkStatus = "draft" | "pending" | "running" | "done" | "failed" | "canceled" | "interrupted";
export type WorkAgent = { character: string; why?: string; channelId?: ID };
export type WorkPhase = {
  id: string;
  title: string;
  goal: string;
  files: string[];
  dependsOn?: string[];
  agents: WorkAgent[];
  wave: number;
  merge?: boolean;
  status?: WorkStatus;
  report?: string;
  error?: string;
};
export type WorkNote = { kind: string; phases?: string[]; detail?: string };
export type WorkPlan = {
  id: ID;
  sessionId: ID;
  prompt: string;
  summary?: string;
  contract?: string;
  phases: WorkPhase[];
  notes?: WorkNote[];
  // the model that wrote the plan
  planner?: { provider: string; model: string } | null;
  status: WorkStatus;
  error?: string;
  // the finished work, checked by running it
  verifying?: boolean;
  verify?: { verified: boolean; checks?: string[]; problems?: string[]; rounds: number } | null;
  createdAt: string;
  updatedAt: string;
};
