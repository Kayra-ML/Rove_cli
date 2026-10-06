// Package teamwork turns one request into several orchestras working
// together: a planner splits the work into parts, each part is an orchestra
// (a conductor and a few members it hands pieces to), parts are wired by
// cables (what one needs from another), and where cables meet the parts'
// work comes together — at the end, in a merge orchestra. A validator keeps
// the plan lean (few parts, few players, no two parallel parts on the same
// files), the user approves it, and the engine starts each part as soon as
// the parts it hangs on are done.
//
// The two failure modes it is built against: too many agents and too many
// headings. The planner is told the rules, and the validator enforces them in
// code, reporting every trim as a note the UI shows.
package teamwork

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// Budgets. A plan never exceeds them, whatever the planner proposes.
const (
	MaxPhases         = 5  // work parts; the merge orchestra comes on top
	MaxAgentsPerPhase = 4  // a conductor and up to three members
	MaxAgents         = 14 // all parts, merge included
	titleRunes        = 60
)

type Status string

const (
	StatusDraft       Status = "draft"
	StatusPending     Status = "pending"
	StatusRunning     Status = "running"
	StatusDone        Status = "done"
	StatusFailed      Status = "failed"
	StatusCanceled    Status = "canceled"
	StatusInterrupted Status = "interrupted" // the daemon stopped mid-run
)

// Agent is one catalog character in a part's orchestra. The first agent of
// a part conducts it; the others are its members.
type Agent struct {
	Character string `json:"character"`
	// Why a member is in the orchestra; the conductor needs no reason.
	Why       string   `json:"why,omitempty"`
	ChannelID types.ID `json:"channelId,omitempty"`
}

type Phase struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Goal  string `json:"goal"`
	// Files are the paths or directories the phase may change.
	Files     []string `json:"files"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Agents    []Agent  `json:"agents"`
	// Wave is 1 for phases that start first; phases of a wave run in parallel.
	Wave   int    `json:"wave"`
	Merge  bool   `json:"merge,omitempty"`
	Status Status `json:"status,omitempty"`
	Report string `json:"report,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Note records something the validator changed, for the UI to explain.
type Note struct {
	Kind   string   `json:"kind"` // merged | tooManyPhases | tooManyAgents | unknownAgent | cycle | solo
	Phases []string `json:"phases,omitempty"`
	Detail string   `json:"detail,omitempty"`
}

// Verification is how the finished work was checked: the commands run and
// what they showed, and what is still wrong. Verified is only true when the
// checks passed — a model saying "done" is not enough.
type Verification struct {
	Verified bool     `json:"verified"`
	Checks   []string `json:"checks,omitempty"`
	Problems []string `json:"problems,omitempty"`
	// Rounds: 1 when it held the first time, 2 after one round of fixes
	Rounds int `json:"rounds"`
}

type Plan struct {
	ID        types.ID `json:"id"`
	SessionID types.ID `json:"sessionId"`
	Prompt    string   `json:"prompt"`
	Summary   string   `json:"summary,omitempty"`
	// Contract is what every phase must agree on (interfaces, names), so
	// parallel phases fit together without reading each other.
	Contract string  `json:"contract,omitempty"`
	Phases   []Phase `json:"phases"`
	Notes    []Note  `json:"notes,omitempty"`
	// Planner is the model that wrote the plan.
	Planner *types.ModelRef `json:"planner,omitempty"`
	Status  Status          `json:"status"`
	Error   string          `json:"error,omitempty"`
	// Verifying is set while the finished work is being checked; Verify is
	// what the check found
	Verifying bool          `json:"verifying,omitempty"`
	Verify    *Verification `json:"verify,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// Agents counts the agents across all phases.
func (p Plan) Agents() int {
	n := 0
	for _, ph := range p.Phases {
		n += len(ph.Agents)
	}
	return n
}

// Draft is what a planner proposes, before validation.
type Draft struct {
	Summary  string       `json:"summary"`
	Contract string       `json:"contract"`
	Phases   []DraftPhase `json:"phases"`
}

type DraftPhase struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Goal      string       `json:"goal"`
	Files     []string     `json:"files"`
	DependsOn []string     `json:"dependsOn"`
	Agents    []DraftAgent `json:"agents"`
}

type DraftAgent struct {
	Character string `json:"character"`
	Why       string `json:"why"`
}

// Catalog tells the validator which characters exist and their role.
type Catalog interface {
	Has(id string) bool
}

// MergeAgent reviews and merges the phases' work.
const MergeAgent = "reviewer"

// Validate turns a draft into a plan that keeps the budgets. It is
// deterministic and does not call a model.
func Validate(d Draft, cat Catalog) (Plan, error) {
	var notes []Note
	type work struct {
		Phase
		orig []string // the planner's ids folded into this phase
	}
	var ws []*work
	for i, dp := range d.Phases {
		id := strings.TrimSpace(dp.ID)
		if id == "" {
			id = fmt.Sprintf("P%d", i+1)
		}
		w := &work{Phase: Phase{
			ID:        id,
			Title:     clipRunes(strings.TrimSpace(dp.Title), titleRunes),
			Goal:      strings.TrimSpace(dp.Goal),
			Files:     cleanFiles(dp.Files),
			DependsOn: dp.DependsOn,
		}, orig: []string{id}}
		if w.Title == "" {
			w.Title = clipRunes(w.Goal, titleRunes)
		}
		seen := map[string]bool{}
		for _, a := range dp.Agents {
			c := strings.TrimSpace(a.Character)
			if seen[c] {
				continue
			}
			seen[c] = true
			if !cat.Has(c) {
				notes = append(notes, Note{Kind: "unknownAgent", Phases: []string{id}, Detail: c})
				continue
			}
			w.Agents = append(w.Agents, Agent{Character: c, Why: strings.TrimSpace(a.Why)})
		}
		if w.Goal == "" && w.Title == "" {
			continue
		}
		ws = append(ws, w)
	}
	if len(ws) == 0 {
		return Plan{}, fmt.Errorf("the plan has no phases")
	}

	// Phases that touch the same files cannot run side by side: fold them
	// into one. Repeat until no two overlap.
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(ws) && !merged; i++ {
			for j := i + 1; j < len(ws); j++ {
				if at, ok := overlap(ws[i].Files, ws[j].Files); ok {
					notes = append(notes, Note{Kind: "merged", Phases: []string{ws[i].ID, ws[j].ID}, Detail: at})
					fold(&ws[i].Phase, ws[j].Phase)
					ws[i].orig = append(ws[i].orig, ws[j].orig...)
					ws = append(ws[:j], ws[j+1:]...)
					merged = true
					break
				}
			}
		}
	}
	// Too many headings: fold the extra phases into the last one kept.
	if len(ws) > MaxPhases {
		var extra []string
		for _, w := range ws[MaxPhases:] {
			extra = append(extra, w.ID)
			fold(&ws[MaxPhases-1].Phase, w.Phase)
			ws[MaxPhases-1].orig = append(ws[MaxPhases-1].orig, w.orig...)
		}
		notes = append(notes, Note{Kind: "tooManyPhases", Phases: extra, Detail: fmt.Sprint(MaxPhases)})
		ws = ws[:MaxPhases]
	}

	// Stable ids F1..Fn, and dependencies mapped onto them.
	idOf := map[string]string{}
	for i, w := range ws {
		nid := fmt.Sprintf("F%d", i+1)
		for _, o := range w.orig {
			idOf[o] = nid
		}
	}
	for i, w := range ws {
		w.ID = fmt.Sprintf("F%d", i+1)
		var deps []string
		seen := map[string]bool{}
		for _, dep := range w.DependsOn {
			n, ok := idOf[strings.TrimSpace(dep)]
			if !ok || n == w.ID || seen[n] {
				continue
			}
			seen[n] = true
			deps = append(deps, n)
		}
		sort.Strings(deps)
		w.DependsOn = deps
		ws[i] = w
	}

	// One conductor per part; members only with a reason; never more than
	// the per-part cap.
	for _, w := range ws {
		if len(w.Agents) == 0 {
			w.Agents = []Agent{{Character: guessOwner(w.Files, cat)}}
		}
		keep := w.Agents[:1]
		for _, a := range w.Agents[1:] {
			if a.Why != "" && len(keep) < MaxAgentsPerPhase {
				keep = append(keep, a)
			}
		}
		if dropped := len(w.Agents) - len(keep); dropped > 0 {
			notes = append(notes, Note{Kind: "tooManyAgents", Phases: []string{w.ID}, Detail: fmt.Sprint(dropped)})
		}
		w.Agents = keep
	}

	phases := make([]Phase, len(ws))
	for i, w := range ws {
		phases[i] = w.Phase
	}
	if cyc := waves(phases); len(cyc) > 0 {
		notes = append(notes, Note{Kind: "cycle", Phases: cyc})
	}

	p := Plan{Summary: strings.TrimSpace(d.Summary), Contract: strings.TrimSpace(d.Contract), Phases: phases, Status: StatusDraft}
	if len(phases) == 1 {
		// one phase is one agent's job: no merge step
		notes = append(notes, Note{Kind: "solo"})
	} else {
		// the merge orchestra counts against the total: over it, the largest
		// orchestra (the later one on a tie) gives up a member, one at a time
		budget := MaxAgents - 1
		cut := map[int]int{}
		for p.Agents() > budget {
			big := -1
			for i := range p.Phases {
				if n := len(p.Phases[i].Agents); n > 1 && (big < 0 || n >= len(p.Phases[big].Agents)) {
					big = i
				}
			}
			if big < 0 {
				break
			}
			p.Phases[big].Agents = p.Phases[big].Agents[:len(p.Phases[big].Agents)-1]
			cut[big]++
		}
		for i := range p.Phases {
			if cut[i] > 0 {
				notes = append(notes, Note{Kind: "tooManyAgents", Phases: []string{p.Phases[i].ID}, Detail: fmt.Sprint(cut[i])})
			}
		}
		last := 0
		var all []string
		for _, ph := range p.Phases {
			if ph.Wave > last {
				last = ph.Wave
			}
			all = append(all, ph.ID)
		}
		p.Phases = append(p.Phases, Phase{
			ID: "M", Title: "Merge", Merge: true, Wave: last + 1, DependsOn: all,
			Goal:   "Put the phases' work together: resolve conflicts, run the build and the tests, fix what breaks.",
			Agents: []Agent{{Character: MergeAgent}},
		})
	}
	// lists go out as [], never null: clients read .length on them
	for i := range p.Phases {
		if p.Phases[i].Files == nil {
			p.Phases[i].Files = []string{}
		}
		if p.Phases[i].Agents == nil {
			p.Phases[i].Agents = []Agent{}
		}
	}
	p.Notes = notes
	return p, nil
}

// waves sets each phase's wave from its dependencies (1 = starts first) and
// drops dependencies that close a cycle, returning the phases involved.
func waves(ps []Phase) []string {
	byID := map[string]int{}
	for i, p := range ps {
		byID[p.ID] = i
	}
	const (
		unseen = iota
		visiting
		done
	)
	state := make([]int, len(ps))
	var cyc []string
	var visit func(i int) int
	visit = func(i int) int {
		switch state[i] {
		case done:
			return ps[i].Wave
		case visiting:
			return 0
		}
		state[i] = visiting
		w := 1
		keep := ps[i].DependsOn[:0]
		for _, dep := range ps[i].DependsOn {
			j := byID[dep]
			if state[j] == visiting {
				cyc = append(cyc, ps[i].ID, dep)
				continue
			}
			if dw := visit(j) + 1; dw > w {
				w = dw
			}
			keep = append(keep, dep)
		}
		ps[i].DependsOn = keep
		ps[i].Wave = w
		state[i] = done
		return w
	}
	for i := range ps {
		visit(i)
	}
	return cyc
}

func fold(into *Phase, from Phase) {
	into.Title = clipRunes(into.Title+" + "+from.Title, titleRunes)
	if from.Goal != "" {
		into.Goal = strings.TrimSpace(into.Goal + " " + from.Goal)
	}
	into.Files = cleanFiles(append(into.Files, from.Files...))
	into.DependsOn = append(into.DependsOn, from.DependsOn...)
	have := map[string]bool{}
	for _, a := range into.Agents {
		have[a.Character] = true
	}
	for _, a := range from.Agents {
		if !have[a.Character] {
			have[a.Character] = true
			if a.Why == "" {
				a.Why = "merged with " + from.ID
			}
			into.Agents = append(into.Agents, a)
		}
	}
}

// overlap reports a path two file sets share: the same file, or a directory
// that contains a path of the other set.
func overlap(a, b []string) (string, bool) {
	for _, x := range a {
		for _, y := range b {
			if x == y || strings.HasPrefix(y, x+"/") {
				return x, true
			}
			if strings.HasPrefix(x, y+"/") {
				return y, true
			}
		}
	}
	return "", false
}

func cleanFiles(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range in {
		f = strings.TrimSpace(strings.ReplaceAll(f, "\\", "/"))
		f = strings.TrimPrefix(f, "./")
		f = strings.TrimSuffix(strings.TrimSuffix(f, "/**"), "/*")
		if f == "" || f == "." {
			continue
		}
		f = path.Clean(f)
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// guessOwner picks a character for a phase the planner left without one,
// from what its files look like.
func guessOwner(files []string, cat Catalog) string {
	j := strings.ToLower(strings.Join(files, " "))
	pick := func(ids ...string) string {
		for _, id := range ids {
			if cat.Has(id) {
				return id
			}
		}
		return ""
	}
	var id string
	switch {
	case strings.Contains(j, "test") || strings.Contains(j, "spec"):
		id = pick("qa")
	case strings.Contains(j, ".md") || strings.Contains(j, "docs"):
		id = pick("writer")
	case strings.Contains(j, ".tsx") || strings.Contains(j, ".jsx") || strings.Contains(j, ".css") || strings.Contains(j, "component"):
		id = pick("frontend")
	case strings.Contains(j, ".go"):
		id = pick("go-backend")
	case strings.Contains(j, ".py"):
		id = pick("python")
	case strings.Contains(j, ".sql") || strings.Contains(j, "migration"):
		id = pick("database")
	}
	if id == "" {
		id = pick("architect", "frontend")
	}
	return id
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// clipTail keeps the end of a report: agents narrate as they work ("first
// I'll look at…") and write the result last, so a report cut from the front
// loses exactly what the next reader needs. The cut lands on a paragraph
// when one starts near it.
func clipTail(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	tail := string(r[len(r)-(n-1):])
	if i := strings.Index(tail, "\n\n"); i >= 0 && i < len(tail)/3 {
		tail = tail[i+2:]
	}
	return "…" + strings.TrimSpace(tail)
}
