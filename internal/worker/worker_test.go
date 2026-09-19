package worker

import (
	"context"
	"testing"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/types"
)

type stub struct{}

func (stub) ID() string                 { return "remote-1" }
func (stub) Capabilities() []string     { return []string{"agent"} }
func (stub) Run(context.Context, agent.RunRequest) (agent.RunResult, error) {
	return agent.RunResult{Assistant: "ok", Done: true}, nil
}
func (stub) Cancel(types.ID) {}

func TestPoolRegisterAndSelect(t *testing.T) {
	p := NewPool(stub{})
	p.Register(stub{})
	if len(p.List()) != 2 {
		t.Fatalf("list %d", len(p.List()))
	}
	w := p.For(types.Card{ID: "c"})
	if w.ID() != "remote-1" {
		t.Fatalf("%s", w.ID())
	}
}
