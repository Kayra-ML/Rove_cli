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
  updatedAt: string;
};

export type Message = {
  id: ID;
  sessionId: ID;
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  createdAt: string;
};

export type Card = {
  id: ID;
  title: string;
  description: string;
  column: Column;
  assigneeAgentId?: ID;
  profile: string;
  model: string;
  dependencies?: ID[];
  acceptanceCriteria?: string[];
  goalId?: ID;
  goalMode: boolean;
  status: string;
  gitBranch?: string;
  worktreePath?: string;
  reviewState?: string;
  workspaceId: ID;
  sessionId?: ID;
  logs?: LogEntry[];
  artifacts?: Artifact[];
};

export type LogEntry = {
  level: string;
  message: string;
  source: string;
  at?: string;
};

export type Artifact = {
  kind: string;
  label: string;
  path?: string;
  url?: string;
};

export type Column = "backlog" | "ready" | "running" | "review" | "done" | "blocked";

export const COLUMNS: Column[] = ["backlog", "ready", "running", "review", "done", "blocked"];

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
  host: string;
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
export type GitStatus = {
  branch: string;
  dirty: boolean;
  changed?: string[];
  untracked?: string[];
};
export type Goal = {
  id: ID;
  title: string;
  status: string;
  iteration: number;
  cardId: ID;
  workspaceId?: ID;
  agentId?: ID;
};

export type AutomationJob = {
  id: ID;
  name: string;
  kind: "sweep_ready" | "assign_idle" | "drive_goals" | string;
  everySeconds: number;
  enabled: boolean;
  lastRunAt?: string;
  lastResult?: string;
};

export type WebhookRule = {
  id: ID;
  name: string;
  secret: string;
  eventType: string;
  agentId: ID;
  enabled: boolean;
  createdAt?: string;
  updatedAt?: string;
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

export type AgentRole = "leader" | "developer" | "reviewer" | "researcher" | "tester" | "designer";

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
  createdAt?: string;
  updatedAt?: string;
};

export type SessionLink = {
  id: ID;
  sessionA: ID;
  sessionB: ID;
  label?: string;
  createdAt?: string;
};

export type RpcResponse<T> = { id?: string; ok: boolean; result?: T; error?: string };
