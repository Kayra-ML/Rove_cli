package codemap

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// Tools returns the agent-facing graph tools bound to svc. Every tool runs
// against the calling agent's workspace.
func Tools(svc *Service) []tool.Tool {
	return []tool.Tool{
		graphTool{svc: svc, name: "graph_query",
			desc:   "Find files by symbol, name or path in the project map.",
			params: `{"q":{"type":"string"},"kind":{"type":"string","enum":["all","file","symbol"]},"limit":{"type":"integer"},"focus":{"type":"array","items":{"type":"string"}}}`,
			run:    runQuery},
		graphTool{svc: svc, name: "graph_neighbors",
			desc:   "A file's imports, importers and callers.",
			params: `{"target":{"type":"string"},"direction":{"type":"string","enum":["both","in","out"]},"limit":{"type":"integer"}}`,
			run:    runNeighbors},
		graphTool{svc: svc, name: "graph_path",
			desc:   "Dependency path between two files.",
			params: `{"from":{"type":"string"},"to":{"type":"string"}}`,
			run:    runPath},
		graphTool{svc: svc, name: "graph_impact",
			desc:   "Files that depend on the given files.",
			params: `{"files":{"type":"array","items":{"type":"string"}},"depth":{"type":"integer"}}`,
			run:    runImpact},
		graphTool{svc: svc, name: "graph_changed",
			desc:   "Changed files (git) and their importers.",
			params: `{"base":{"type":"string"},"limit":{"type":"integer"}}`,
			run:    runChanged},
	}
}

type graphTool struct {
	svc    *Service
	name   string
	desc   string
	params string
	run    func(ix *Index, args json.RawMessage) (any, error)
}

func (g graphTool) Name() string        { return g.name }
func (g graphTool) Description() string { return g.desc }
func (g graphTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":` + g.params + `,"additionalProperties":false}`)
}
func (g graphTool) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (g graphTool) Call(_ context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var out any
	err := g.svc.With(tc.Workspace, func(ix *Index) error {
		var err error
		out, err = g.run(ix, args)
		return err
	})
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	b, _ := json.Marshal(out)
	return tool.Result{Content: string(b)}, nil
}

func runQuery(ix *Index, args json.RawMessage) (any, error) {
	var in struct {
		Q     string   `json:"q"`
		Kind  string   `json:"kind"`
		Limit int      `json:"limit"`
		Focus []string `json:"focus"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return ix.Query(in.Q, in.Kind, in.Limit, in.Focus), nil
}

func runNeighbors(ix *Index, args json.RawMessage) (any, error) {
	var in struct {
		Target    string `json:"target"`
		Direction string `json:"direction"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	res, ok := ix.Neighbors(in.Target, in.Direction, in.Limit)
	if !ok {
		return map[string]any{"ok": false, "error": "not in graph: " + in.Target, "hint": suggest(ix, in.Target)}, nil
	}
	return res, nil
}

func runPath(ix *Index, args json.RawMessage) (any, error) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	p, ok := ix.Path(in.From, in.To)
	if !ok {
		return map[string]any{"ok": false, "error": "no path"}, nil
	}
	return map[string]any{"ok": true, "path": p, "hops": len(p) - 1}, nil
}

func runImpact(ix *Index, args json.RawMessage) (any, error) {
	var in struct {
		Files []string `json:"files"`
		Depth int      `json:"depth"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	hits := ix.Impact(in.Files, in.Depth)
	return map[string]any{"count": len(hits), "files": headStrings(hits, 60)}, nil
}

func runChanged(ix *Index, args json.RawMessage) (any, error) {
	var in struct {
		Base  string `json:"base"`
		Limit int    `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
	}
	return map[string]any{"files": ix.Changed(in.Base, in.Limit)}, nil
}

func suggest(ix *Index, target string) []string {
	q := ix.Query(strings.TrimSuffix(target, ".go"), "all", 5, nil)
	var out []string
	for _, h := range q.Hits {
		out = append(out, h.File)
	}
	return out
}
