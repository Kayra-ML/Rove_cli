// Package team lets a chat split its work between subagents: the chat's
// model hands tasks out with the team_delegate tool, each task runs in a
// channel (child session) of its own with a fresh context, and only its
// short report comes back. Subagents are plain agents — no characters, no
// standing members: a chat in Orchestra mode splits the work, it does not
// keep a cast. (Characters play in Teamwork's orchestras.)
//
// Token budget matters here: a subagent only ever sees the task it was
// given, and the lead only gets each one's clipped report back — never a
// subagent's full transcript.
package team

import (
	"context"

	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type Team struct {
	st   *store.Store
	sess *session.Manager
}

func New(st *store.Store, sm *session.Manager) *Team { return &Team{st: st, sess: sm} }

// Brief is what a chat's model is told about handing work to subagents.
// Empty for a channel: a subagent works, it does not hand work on (a
// Teamwork conductor's members are given their pieces by the engine).
func (t *Team) Brief(ctx context.Context, sessionID types.ID) string {
	s, err := t.st.GetSession(ctx, sessionID)
	if err != nil || s.ParentID != "" {
		return ""
	}
	return delegationGuide
}

// delegationGuide is when to hand work to a subagent and how, after Hermes
// Agent's delegation patterns. A subagent is cheap for the lead's context and
// costly in every other way, so the guide is as much about when not to.
const delegationGuide = `## Subagents
You can hand work to subagents with team_delegate. Each starts with a fresh context: it knows nothing of this conversation, so every task has to carry what it needs — absolute file paths, the constraint, the command that proves it works, what done looks like. Only its short report comes back to you.
Delegate when parts are independent (different files or modules) — several in one call run at the same time — or when a part would flood your context (a broad search, a long investigation).
Do it yourself when it is one quick edit, when it needs the user, or when a step depends on another's result: then call once, read the reports, and call again.
Two tasks must never change the same file; give that file to one of them.
Reports are the subagents' own accounts: read the diff or run the tests before you tell the user it is done.`
