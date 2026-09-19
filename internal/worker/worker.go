package worker

import (
	"context"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/types"
)

// Worker executes agent runs. The local daemon is the default worker.
// A future remote worker implements the same interface over the RPC protocol
// without changing Goal, Kanban, or Orchestrator.
type Worker interface {
	ID() string
	Capabilities() []string
	Run(ctx context.Context, req agent.RunRequest) (agent.RunResult, error)
	Cancel(agentID types.ID)
}

type Local struct {
	Name string
	RT   *agent.Runtime
}

func (l Local) ID() string { return l.Name }
func (l Local) Capabilities() []string {
	return []string{"agent", "tools", "terminal", "git"}
}
func (l Local) Run(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
	return l.RT.Run(ctx, req)
}
func (l Local) Cancel(agentID types.ID) { l.RT.Cancel(agentID) }

// Pool selects a worker for a card. Today it always returns the local worker;
// remote registration is a compatible extension.
type Pool struct {
	local Worker
	all   []Worker
}

func NewPool(local Worker) *Pool {
	return &Pool{local: local, all: []Worker{local}}
}

func (p *Pool) Register(w Worker) { p.all = append(p.all, w) }

func (p *Pool) For(_ types.Card) Worker { return p.local }

func (p *Pool) List() []Worker { return append([]Worker(nil), p.all...) }
