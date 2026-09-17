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

const (
	MethodPing              = "ping"
	MethodShutdown          = "shutdown"
	MethodAgentList         = "agent.list"
	MethodAgentUpsert       = "agent.upsert"
	MethodSessionCreate     = "session.create"
	MethodSessionList       = "session.list"
	MethodSessionHistory    = "session.history"
	MethodSessionSend       = "session.send"
	MethodSessionRename     = "session.rename"
	MethodSessionDelete     = "session.delete"
	MethodSessionTruncate   = "session.truncate"
	MethodSessionAppend     = "session.append"
	MethodAgentCancel       = "agent.cancel"
	MethodSkillSetEnabled   = "skill.setEnabled"
	MethodWorkspaceDelete   = "workspace.delete"
	MethodProviderDelete    = "provider.delete"
	MethodCardList          = "card.list"
	MethodCardCreate        = "card.create"
	MethodCardMove          = "card.move"
	MethodCardGet           = "card.get"
	MethodCardReview        = "card.review"
	MethodCardDispatch      = "card.dispatch"
	MethodCardDelete        = "card.delete"
	MethodCardAssign        = "card.assign"
	MethodGoalCreate        = "goal.create"
	MethodGoalDelete        = "goal.delete"
	MethodGoalList          = "goal.list"
	MethodGoalDrive         = "goal.drive"
	MethodGoalGet           = "goal.get"
	MethodWorkspaceOpen     = "workspace.open"
	MethodWorkspaceList     = "workspace.list"
	MethodTerminalSpawn     = "terminal.spawn"
	MethodTerminalList      = "terminal.list"
	MethodTerminalWrite     = "terminal.write"
	MethodTerminalResize    = "terminal.resize"
	MethodTerminalKill      = "terminal.kill"
	MethodTerminalRestart   = "terminal.restart"
	MethodTerminalAttach    = "terminal.attach"
	MethodTerminalDetach    = "terminal.detach"
	MethodSSHOpen           = "ssh.open"
	MethodGitStatus         = "git.status"
	MethodGitDiff           = "git.diff"
	MethodSkillList         = "skill.list"
	MethodSkillInstall      = "skill.install"
	MethodSkillUninstall    = "skill.uninstall"
	MethodMarketList        = "market.list"
	MethodMarketPublish     = "market.publish"
	MethodMarketInstall     = "market.install"
	MethodProviderList      = "provider.list"
	MethodProviderUpsert    = "provider.upsert"
	MethodSecretPut         = "secret.put"
	MethodPermissionList    = "permission.list"
	MethodPermissionPut     = "permission.put"
	MethodMemoryPut         = "memory.put"
	MethodMemoryList        = "memory.list"
	MethodSubscribe         = "events.subscribe"
	MethodFileTree          = "fs.tree"
	MethodFileRead          = "fs.read"
	MethodCardLogs          = "card.logs"
	MethodCardAddArtifact   = "card.addArtifact"
	MethodAgentDelete       = "agent.delete"
	MethodGitCommit         = "git.commit"
	MethodGitLog            = "git.log"
	MethodHealthStream      = "health.stream"

	// Harness policy methods.
	MethodHarnessGet        = "harness.get"
	MethodHarnessSet        = "harness.set"
	MethodHarnessCompose    = "harness.compose"
	MethodHarnessMutations  = "harness.mutations"
	MethodHarnessPresets    = "harness.presets"
	MethodUsageGet          = "usage.get"

	MethodAutomationList    = "automation.list"
	MethodAutomationUpsert  = "automation.upsert"
	MethodAutomationDelete  = "automation.delete"
	MethodAutomationTick    = "automation.tick"

	// Checkpoint / snapshot.
	MethodCheckpointTake    = "checkpoint.take"
	MethodCheckpointList    = "checkpoint.list"
	MethodCheckpointRestore = "checkpoint.restore"
	MethodCheckpointDrop    = "checkpoint.drop"

	// Diff hunk accept/reject.
	MethodGitApplyHunk  = "git.applyHunk"
	MethodGitRejectHunk = "git.rejectHunk"

	// Session export/import.
	MethodSessionExport = "session.export"
	MethodSessionImport = "session.import"

	// Git branch / push / PR.
	MethodGitBranch = "git.branch"
	MethodGitPush   = "git.push"
	MethodGitPR     = "git.pr"

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

	// Cost / billing.
	MethodCostGet        = "cost.get"
	MethodCostPriceTable = "cost.priceTable"
)
