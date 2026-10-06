package teamwork

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CharacterLine is how the planner sees a catalog character: one short line.
type CharacterLine struct {
	ID, Name, Summary string
}

// Budget on what the planner reads about the project.
const (
	projectDepth   = 2
	projectEntries = 120
)

// PlannerPrompt is the planner's system prompt: the rules that keep a plan
// small, and the characters it may staff it with.
func PlannerPrompt(chars []CharacterLine) string {
	var b strings.Builder
	b.WriteString(`You plan work for several small orchestras of AI coding agents. Each part of the work is one
orchestra: a conductor who owns the part and hands pieces of it to the orchestra's members, who work in parallel.
Parts are wired together: a part that needs another's output depends on it. Reply with JSON only:
{"summary": "one line", "contract": "decisions every part must share (names, interfaces), short; empty if none",
 "phases": [{"id": "F1", "title": "under 60 chars", "goal": "what this part delivers",
   "files": ["paths or directories it will change"], "dependsOn": ["ids of parts that must finish first"],
   "agents": [{"character": "id of the conductor"}, {"character": "id", "why": "what this member does in the part"}]}]}
Rules:
- Fewer is better. A small or single-area task is ONE part with a conductor alone.
- At most 5 parts. Split only where parts touch different files and can be done independently.
- Two parts must never change the same files.
- The first agent of a part conducts it. Add members (at most 3) only when the part has pieces that can be done
  side by side, and say for each what it does.
- No review, test-only or "integrate" part: a merge orchestra is added after your parts automatically.
- Use dependsOn only when a part truly needs another's output; otherwise parts run in parallel. A part may depend on
  several parts: their work meets there.
Characters (id — name — strength):
`)
	for _, c := range chars {
		fmt.Fprintf(&b, "- %s — %s — %s\n", c.ID, c.Name, c.Summary)
	}
	return b.String()
}

// PlannerRequest is the user message: the task and a bounded look at the
// project so the plan names real paths.
func PlannerRequest(task, projectTree string) string {
	if projectTree == "" {
		return "Task:\n" + strings.TrimSpace(task)
	}
	return "Task:\n" + strings.TrimSpace(task) + "\n\nProject files (partial):\n" + projectTree
}

// ParseDraft reads the planner's reply: the first JSON object in it, even
// when wrapped in prose or a code fence.
func ParseDraft(reply string) (Draft, error) {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return Draft{}, fmt.Errorf("the planner did not return a plan")
	}
	var d Draft
	if err := json.Unmarshal([]byte(reply[start:end+1]), &d); err != nil {
		return Draft{}, fmt.Errorf("the planner's plan is not valid JSON: %w", err)
	}
	if len(d.Phases) == 0 {
		return Draft{}, fmt.Errorf("the planner returned no phases")
	}
	return d, nil
}

// ProjectTree lists the project two levels deep (directories marked with a
// trailing slash), skipping dependency and build folders, up to a fixed
// number of entries — enough to name real paths, cheap in tokens.
func ProjectTree(root string) string {
	if root == "" {
		return ""
	}
	skip := map[string]bool{".git": true, "node_modules": true, "dist": true, "build": true, "vendor": true, ".next": true, "target": true, "__pycache__": true, ".venv": true}
	var lines []string
	var walk func(dir, rel string, depth int)
	walk = func(dir, rel string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if len(lines) >= projectEntries {
				return
			}
			name := e.Name()
			if skip[name] || (strings.HasPrefix(name, ".") && name != ".github") {
				continue
			}
			p := name
			if rel != "" {
				p = rel + "/" + name
			}
			if e.IsDir() {
				lines = append(lines, p+"/")
				if depth < projectDepth {
					walk(filepath.Join(dir, name), p, depth+1)
				}
			} else {
				lines = append(lines, p)
			}
		}
	}
	walk(root, "", 1)
	return strings.Join(lines, "\n")
}
