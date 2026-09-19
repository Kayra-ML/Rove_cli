package tool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/types"
)

type GitStatusTool struct{ Git *gitwt.Manager }

func (GitStatusTool) Name() string        { return "git_status" }
func (GitStatusTool) Description() string { return "Show git status of the workspace repository." }
func (GitStatusTool) Parameters() json.RawMessage {
	return schema(`{}`)
}
func (GitStatusTool) RequiredPermission() types.PermissionAction { return types.PermGit }
func (t GitStatusTool) Call(_ context.Context, tc Context, _ json.RawMessage) (Result, error) {
	g := t.Git
	if g == nil {
		g = gitwt.New()
	}
	st, err := g.Status(tc.Workspace)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	b, _ := json.Marshal(st)
	return Result{Content: string(b)}, nil
}

type GitCommitTool struct{ Git *gitwt.Manager }

func (GitCommitTool) Name() string        { return "git_commit" }
func (GitCommitTool) Description() string { return "Stage all and commit in the workspace (or worktree)." }
func (GitCommitTool) Parameters() json.RawMessage {
	return schema(`{"message":{"type":"string"}}`)
}
func (GitCommitTool) RequiredPermission() types.PermissionAction { return types.PermGit }
func (t GitCommitTool) Call(_ context.Context, tc Context, args json.RawMessage) (Result, error) {
	var in struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	if strings.TrimSpace(in.Message) == "" {
		in.Message = "aether: agent commit"
	}
	g := t.Git
	if g == nil {
		g = gitwt.New()
	}
	if err := g.AddAll(tc.Workspace); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	if err := g.Commit(tc.Workspace, in.Message); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	return Result{Content: "committed"}, nil
}
