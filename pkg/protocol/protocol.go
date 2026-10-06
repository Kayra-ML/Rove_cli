package protocol

import "encoding/json"

type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	Token  string          `json:"token,omitempty"`
}

type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Event  *EventFrame     `json:"event,omitempty"`
}

type EventFrame struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// APILevel is raised whenever the daemon gains methods the app relies on.
// The app compares it with the daemon's (health) to tell an out-of-date
// daemon — on this computer or on a server — apart from a broken one.
const APILevel = 18

const (
	MethodPing            = "ping"
	MethodShutdown        = "shutdown"
	MethodAgentList       = "agent.list"
	MethodAgentUpsert     = "agent.upsert"
	MethodSessionCreate   = "session.create"
	MethodSessionList     = "session.list"
	MethodSessionHistory  = "session.history"
	MethodSessionSend     = "session.send"
	MethodSessionRename   = "session.rename"
	MethodSessionDelete   = "session.delete"
	MethodSessionTruncate = "session.truncate"
	MethodSessionAppend   = "session.append"
	MethodSkillSetEnabled = "skill.setEnabled"
	MethodWorkspaceDelete = "workspace.delete"
	MethodProviderDelete  = "provider.delete"
	MethodGoalCreate      = "goal.create"
	MethodGoalDelete      = "goal.delete"
	MethodGoalList        = "goal.list"
	MethodGoalDrive       = "goal.drive"

	// Reviewing a turn's changes, compacting a chat, reasoning effort, and
	// the project's rule files.
	MethodEditsList        = "edits.list"
	MethodEditsAccept      = "edits.accept"
	MethodEditsRevert      = "edits.revert"
	MethodSessionCompact   = "session.compact"
	MethodSessionSetEffort = "session.setEffort"
	MethodWorkspaceRules   = "workspace.rules"
	MethodGoalGet          = "goal.get"
	MethodWorkspaceOpen    = "workspace.open"
	MethodWorkspaceList    = "workspace.list"
	MethodTerminalSpawn    = "terminal.spawn"
	MethodTerminalList     = "terminal.list"
	MethodTerminalWrite    = "terminal.write"
	MethodTerminalResize   = "terminal.resize"
	MethodTerminalRestart  = "terminal.restart"
	MethodTerminalAttach   = "terminal.attach"
	MethodTerminalDetach   = "terminal.detach"
	MethodSSHOpen          = "ssh.open"
	// the SSH servers this machine already knows (~/.ssh/config, known_hosts)
	MethodSSHDiscover = "ssh.discover"
	// the folders of the daemon's machine, for picking a project on a server
	MethodFileDirs = "fs.dirs"

	// A session's terminals (terminal mode): list them, open one, hand one
	// a message, and keep the deck's layout with the session.
	MethodTerminalPanes     = "terminal.panes"
	MethodTerminalNewPane   = "terminal.newPane"
	MethodTerminalSendPane  = "terminal.sendPane"
	MethodTerminalLayoutGet = "terminal.layout.get"
	MethodTerminalLayoutSet = "terminal.layout.set"
	MethodSkillList         = "skill.list"
	MethodSkillInstall      = "skill.install"
	MethodSkillUninstall    = "skill.uninstall"
	MethodMarketList        = "market.list"
	MethodProviderList      = "provider.list"
	MethodProviderUpsert    = "provider.upsert"
	// ask a saved provider for its models again
	MethodProviderRefresh = "provider.refreshModels"
	MethodSecretPut       = "secret.put"
	MethodPermissionList  = "permission.list"
	MethodPermissionPut   = "permission.put"
	// Permission questions waiting for the user, and their answers.
	MethodPermissionAsks   = "permission.asks"
	MethodPermissionAnswer = "permission.answer"
	MethodMemoryPut        = "memory.put"
	MethodMemoryList       = "memory.list"
	MethodFileTree         = "fs.tree"
	MethodFileRead         = "fs.read"
	MethodAgentDelete      = "agent.delete"

	// Harness policy methods.
	MethodUsageGet = "usage.get"
	// usage.report: tokens per day and per model over a range of days, by
	// Rove's own count and by the providers' reports (see internal/usage)
	MethodUsageReport = "usage.report"

	// Automation.
	MethodAutomationList      = "automation.list"
	MethodAutomationUpsert    = "automation.upsert"
	MethodAutomationDelete    = "automation.delete"
	MethodAutomationTick      = "automation.tick"
	MethodAutomationCatalog   = "automation.catalog"
	MethodAutomationInstall   = "automation.install"
	MethodAutomationUninstall = "automation.uninstall"

	// Git branch / push / PR.

	// Checkpoint / snapshot.
	MethodCheckpointTake    = "checkpoint.take"
	MethodCheckpointList    = "checkpoint.list"
	MethodCheckpointRestore = "checkpoint.restore"

	// Diff hunk accept/reject.

	// Session export / import.
	MethodSessionExport = "session.export"

	// MCP server registry.
	MethodMCPList     = "mcp.list"
	MethodMCPAdd      = "mcp.add"
	MethodMCPRemove   = "mcp.remove"
	MethodMCPDiscover = "mcp.discover"

	// Webhook trigger rules.
	MethodWebhookList   = "webhook.list"
	MethodWebhookUpsert = "webhook.upsert"
	MethodWebhookDelete = "webhook.delete"

	// Codebase FTS5 index.
	MethodIndexBuild  = "index.build"
	MethodIndexSearch = "index.search"

	// Multi-profile role system.
	MethodProfileList       = "profile.list"
	MethodProfileUpsert     = "profile.upsert"
	MethodProfileDelete     = "profile.delete"
	MethodProfileSetDefault = "profile.setDefault"

	// Session linking and relay.
	MethodSessionLink   = "session.link"
	MethodSessionUnlink = "session.unlink"
	MethodSessionLinked = "session.linked"
	MethodSessionRelay  = "session.relay"

	// Project map (CodeMap port).
	MethodMapStatus    = "codemap.status"
	MethodMapBuild     = "codemap.build"
	MethodMapGraph     = "codemap.graph"
	MethodMapQuery     = "codemap.query"
	MethodMapNeighbors = "codemap.neighbors"
	MethodMapImpact    = "codemap.impact"
	MethodMapShare     = "codemap.share"

	// Context map: one canvas of sessions joined by cables.
	MethodCtxGet        = "ctxmap.get"
	MethodCtxPlace      = "ctxmap.place"
	MethodCtxRemove     = "ctxmap.remove"
	MethodCtxLink       = "ctxmap.link"
	MethodCtxUpdateLink = "ctxmap.updateLink"
	MethodCtxUnlink     = "ctxmap.unlink"
	MethodCtxSend       = "ctxmap.send"
	MethodCtxRelays     = "ctxmap.relays"
	// MethodCtxAssistant returns the chat's context-map assistant, a hidden
	// child session, making it on first use.
	MethodCtxAssistant = "ctxmap.assistant"

	// Connections: the systems a user can connect their own account to.
	// catalog lists them with what is installed and signed in; terminal
	// opens the system terminal for an install or a browser sign-in; add
	// saves an agent system as a provider.
	MethodConnectCatalog  = "connect.catalog"
	MethodConnectTerminal = "connect.terminal"
	MethodConnectAdd      = "connect.add"

	// Session personas: expert characters, profiles and feature toggles.
	MethodPersonaCatalog = "persona.catalog"
	MethodPersonaGet     = "persona.get"
	MethodPersonaSet     = "persona.set"
	MethodPersonaClear   = "persona.clear"
	MethodPersonaBadges  = "persona.badges"
	MethodSessionCancel  = "session.cancel"

	// Subagents: the work a chat handed out with team_delegate, and what the
	// user can do to one while it runs or after (see internal/team).
	MethodSubagentList    = "subagent.list"
	MethodSubagentStop    = "subagent.stop"
	MethodSubagentSteer   = "subagent.steer"
	MethodSubagentApply   = "subagent.apply"
	MethodSubagentDiscard = "subagent.discard"

	// Teamwork: a planned, phased team effort in a chat (see internal/teamwork).
	MethodWorkPlan        = "teamwork.plan"
	MethodWorkGet         = "teamwork.get"
	MethodWorkRemovePhase = "teamwork.removePhase"
	MethodWorkRemoveAgent = "teamwork.removeAgent"
	MethodWorkApprove     = "teamwork.approve"
	MethodWorkCancel      = "teamwork.cancel"
	MethodWorkDiscard     = "teamwork.discard"
	MethodWorkRetry       = "teamwork.retry"

	// Staff: the Agent space's team — who is working on what, tasks handed
	// out with a report back, and each agent's notes (see internal/staff).
	MethodStaffMembers    = "staff.members"
	MethodStaffAssign     = "staff.assign"
	MethodStaffTasks      = "staff.tasks"
	MethodStaffSeen       = "staff.seen"
	MethodStaffStop       = "staff.stop"
	MethodStaffNoteAdd    = "staff.noteAdd"
	MethodStaffNoteDelete = "staff.noteDelete"
	// watches: an agent watching a chat on the Session Map
	MethodStaffWatches     = "staff.watches"
	MethodStaffWatchSave   = "staff.watchSave"
	MethodStaffWatchDelete = "staff.watchDelete"
	// schedules: an agent given a task on a timer
	MethodStaffSchedules      = "staff.schedules"
	MethodStaffScheduleSave   = "staff.scheduleSave"
	MethodStaffScheduleDelete = "staff.scheduleDelete"
	// a task's changes, kept in its own checkout: see, apply, throw away,
	// or open as a pull request
	MethodStaffDiff    = "staff.diff"
	MethodStaffApply   = "staff.apply"
	MethodStaffDiscard = "staff.discard"
	MethodStaffPR      = "staff.pr"
	// monitors: a command checked on a timer, a task when it shows something new
	MethodStaffMonitors       = "staff.monitors"
	MethodStaffMonitorSave    = "staff.monitorSave"
	MethodStaffMonitorDelete  = "staff.monitorDelete"
	MethodStaffMonitorPresets = "staff.monitorPresets"
	// handoffs: one agent's finished work handed on to another
	MethodStaffHandoffs      = "staff.handoffs"
	MethodStaffHandoffSave   = "staff.handoffSave"
	MethodStaffHandoffDelete = "staff.handoffDelete"

	// Models: what every configured provider offers, and a chat's own pick.
	MethodModelList       = "model.list"
	MethodSessionModel    = "session.model"
	MethodSessionSetModel = "session.setModel"

	// Agent role management.
)
