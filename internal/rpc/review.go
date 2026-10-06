package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/diffx"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// editFile is one changed file of a turn, for review.
type editFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"`  // pending | accepted | reverted
	Change  string `json:"change"`  // added | modified | deleted | unchanged
	Added   int    `json:"added"`   // lines
	Removed int    `json:"removed"` // lines
	Diff    string `json:"diff"`
}

type editsView struct {
	RunID types.ID   `json:"runId"`
	Files []editFile `json:"files"`
}

func (s *Server) handleReview(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	var p struct {
		SessionID types.ID `json:"sessionId"`
		RunID     types.ID `json:"runId"`
		Path      string   `json:"path"`
		AgentID   types.ID `json:"agentId"`
		Effort    string   `json:"effort"`
		Workspace string   `json:"workspace"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	a := s.app
	switch req.Method {
	case protocol.MethodEditsList:
		v, err := s.editsView(ctx, p.SessionID, p.RunID)
		return core.MustJSON(v), err

	case protocol.MethodEditsAccept, protocol.MethodEditsRevert:
		edits, err := a.Store.TurnEdits(ctx, p.SessionID, p.RunID)
		if err != nil {
			return nil, err
		}
		if len(edits) == 0 {
			return nil, errors.New("no changes to review")
		}
		for _, e := range edits {
			if (p.Path != "" && e.Path != p.Path) || e.Status == "reverted" {
				continue
			}
			if req.Method == protocol.MethodEditsRevert {
				if err := revertEdit(e); err != nil {
					return nil, fmt.Errorf("%s: %w", e.Path, err)
				}
				_ = a.Store.SetTurnEditStatus(ctx, e.RunID, e.Path, "reverted")
			} else if e.Status == "pending" {
				_ = a.Store.SetTurnEditStatus(ctx, e.RunID, e.Path, "accepted")
			}
		}
		v, err := s.editsView(ctx, p.SessionID, edits[0].RunID)
		return core.MustJSON(v), err

	case protocol.MethodSessionCompact:
		agentID := p.AgentID
		if agentID == "" {
			sess, err := a.Sess.Get(ctx, p.SessionID)
			if err != nil {
				return nil, err
			}
			agentID = sess.AgentID
		}
		ok, err := a.Agents.Compact(ctx, p.SessionID, agentID)
		if ok {
			a.AgentSessions.Forget(string(p.SessionID))
		}
		return core.MustJSON(map[string]any{"compacted": ok}), err

	case protocol.MethodSessionSetEffort:
		switch p.Effort {
		case "", "low", "medium", "high":
		default:
			return nil, fmt.Errorf("effort must be low, medium or high")
		}
		if _, err := a.Store.GetSession(ctx, p.SessionID); err != nil {
			return nil, fmt.Errorf("session not found")
		}
		err := a.Store.SetSessionEffort(ctx, p.SessionID, p.Effort)
		return core.MustJSON(map[string]any{"ok": err == nil, "effort": p.Effort}), err

	case protocol.MethodWorkspaceRules:
		dir := p.Workspace
		if dir == "" && p.SessionID != "" {
			if sess, err := a.Sess.Get(ctx, p.SessionID); err == nil && sess.WorkspaceID != "" {
				if ws, err := a.WS.Get(ctx, sess.WorkspaceID); err == nil {
					dir = ws.Path
				}
			}
		}
		_, files := agent.ProjectRules(dir)
		if files == nil {
			files = []string{}
		}
		return core.MustJSON(map[string]any{"files": files}), nil
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}

// editsView diffs each file a run changed against what it held before.
func (s *Server) editsView(ctx context.Context, sessionID, runID types.ID) (editsView, error) {
	edits, err := s.app.Store.TurnEdits(ctx, sessionID, runID)
	if err != nil || len(edits) == 0 {
		return editsView{Files: []editFile{}}, err
	}
	v := editsView{RunID: edits[0].RunID, Files: []editFile{}}
	for _, e := range edits {
		now, exists := readRel(e.Workspace, e.Path)
		f := editFile{Path: e.Path, Status: e.Status}
		switch {
		case !e.Existed && exists:
			f.Change = "added"
		case e.Existed && !exists:
			f.Change = "deleted"
		case now == e.Before:
			f.Change = "unchanged"
		default:
			f.Change = "modified"
		}
		var st diffx.Stats
		f.Diff, st = diffx.Unified(e.Path, e.Before, now)
		f.Added, f.Removed = st.Added, st.Removed
		v.Files = append(v.Files, f)
	}
	return v, nil
}

// inside resolves a stored relative path in its workspace, refusing any
// path that would leave it.
func inside(workspace, rel string) (string, error) {
	abs := filepath.Clean(filepath.Join(workspace, filepath.FromSlash(rel)))
	r, err := filepath.Rel(workspace, abs)
	if err != nil || r == "." || strings.HasPrefix(r, "..") {
		return "", errors.New("path outside the workspace")
	}
	return abs, nil
}

func readRel(workspace, rel string) (string, bool) {
	abs, err := inside(workspace, rel)
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// revertEdit puts a file back as it was before the run: its old content,
// or gone if the run created it.
func revertEdit(e types.TurnEdit) error {
	abs, err := inside(e.Workspace, e.Path)
	if err != nil {
		return err
	}
	if !e.Existed {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(e.Before), mode)
}

// Images a message may carry: a few, each a data: URL of an image type and
// of a size a vision model takes.
const (
	maxImages     = 4
	maxImageBytes = 5 << 20
)

func checkImages(imgs []string) error {
	if len(imgs) > maxImages {
		return fmt.Errorf("at most %d images per message", maxImages)
	}
	for _, img := range imgs {
		if !strings.HasPrefix(img, "data:image/") || !strings.Contains(img, ";base64,") {
			return errors.New("images must be data:image/…;base64 URLs")
		}
		if len(img) > maxImageBytes*4/3+64 {
			return fmt.Errorf("image too large (max %d MB)", maxImageBytes>>20)
		}
	}
	return nil
}
