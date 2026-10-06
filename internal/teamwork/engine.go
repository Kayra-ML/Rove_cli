package teamwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

// EventUpdated fires whenever a chat's plan changes (drafted, edited, a
// phase moves on, finished), on the chat's topic.
const EventUpdated types.EventType = "teamwork.updated"

// Token budget for what agents pass to each other.
const (
	reportClip  = 1200 // a part's report, as the parts after it read it
	handoffClip = 900  // a member's report, as its conductor reads it
	historyTail = 12   // messages of its own channel an agent re-reads per turn
)

type (
	Runner func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error)
	// Asker asks a model once, without tools (an empty ModelRef: the
	// agent's own model) and says which model answered.
	Asker func(ctx context.Context, agentID types.ID, m types.ModelRef, system, user string) (string, types.ModelRef, error)
)

// Engine plans, stores and runs Teamwork plans. Plans are per chat: a chat
// has one current plan (its latest).
type Engine struct {
	st  *store.Store
	sm  *session.Manager
	bus *eventbus.Bus

	Ask           Asker
	Run           Runner
	Cat           Catalog
	Characters    func() []CharacterLine
	Name          func(character string) string
	WorkspacePath func(ctx context.Context, ws types.ID) string
	// ChatModel is the model picked for a chat (/models), if any: the
	// planner's default.
	ChatModel func(ctx context.Context, sessionID types.ID) types.ModelRef

	mu      sync.Mutex
	running map[types.ID]*run
}

type run struct {
	plan   *Plan
	cancel context.CancelFunc
}

func New(st *store.Store, sm *session.Manager, bus *eventbus.Bus) *Engine {
	return &Engine{st: st, sm: sm, bus: bus, running: map[types.ID]*run{}}
}

// Plan asks the planner for a plan and keeps it as a draft for the user to
// approve. A planner reply that is not a usable plan becomes a one-phase
// plan (with a note saying so) rather than an error: the user can still run
// or re-plan it.
func (e *Engine) Plan(ctx context.Context, sessionID types.ID, prompt string) (Plan, error) {
	return e.PlanWith(ctx, sessionID, prompt, types.ModelRef{})
}

// PlanWith is Plan written by a chosen model; an empty planner uses the
// chat's model, then the agent's.
func (e *Engine) PlanWith(ctx context.Context, sessionID types.ID, prompt string, planner types.ModelRef) (Plan, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Plan{}, errors.New("empty request")
	}
	home, err := e.sm.Get(ctx, sessionID)
	if err != nil {
		return Plan{}, err
	}
	if home.ParentID != "" {
		return Plan{}, errors.New("plans belong to a chat, not a channel")
	}
	if e.busy(sessionID) {
		return Plan{}, errors.New("a plan is already running in this chat")
	}
	var chars []CharacterLine
	if e.Characters != nil {
		chars = e.Characters()
	}
	if planner.Model == "" && e.ChatModel != nil {
		planner = e.ChatModel(ctx, sessionID)
	}
	reply, used, askErr := e.Ask(ctx, home.AgentID, planner, PlannerPrompt(chars), PlannerRequest(prompt, ProjectTree(e.wsPath(ctx, home.WorkspaceID))))
	if askErr != nil && ctx.Err() != nil {
		return Plan{}, askErr
	}
	var fallback string
	d, perr := ParseDraft(reply)
	switch {
	case askErr != nil:
		fallback = askErr.Error()
	case perr != nil:
		fallback = perr.Error()
	}
	if fallback != "" {
		d = Draft{Summary: clipRunes(prompt, 120)}
		d.Phases = []DraftPhase{{ID: "F1", Title: clipRunes(prompt, titleRunes), Goal: prompt}}
	}
	p, err := Validate(d, e.Cat)
	if err != nil {
		return Plan{}, err
	}
	if fallback != "" {
		p.Notes = append([]Note{{Kind: "plannerFallback", Detail: fallback}}, p.Notes...)
	}
	if used.Model != "" {
		p.Planner = &used
	}
	// a new request replaces a draft nobody approved; finished plans stay
	var stale types.ID
	if prev, err := e.Get(ctx, sessionID); err == nil && prev.Status == StatusDraft {
		stale = prev.ID
	}
	now := time.Now().UTC()
	p.ID, p.SessionID, p.Prompt, p.CreatedAt, p.UpdatedAt = id.NewID(), sessionID, prompt, now, now
	if err := e.save(ctx, &p); err != nil {
		return Plan{}, err
	}
	if stale != "" {
		_ = e.st.DeleteTeamworkPlan(ctx, stale)
	}
	e.publish(p)
	return p, nil
}

// Get returns a chat's current plan. A plan left "running" by a daemon that
// has since stopped is reported as interrupted.
func (e *Engine) Get(ctx context.Context, sessionID types.ID) (Plan, error) {
	body, err := e.st.LatestTeamworkPlan(ctx, sessionID)
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return Plan{}, err
	}
	e.mu.Lock()
	r, live := e.running[p.ID]
	e.mu.Unlock()
	if live {
		return e.snapshot(r), nil
	}
	if p.Status == StatusRunning {
		p.Status = StatusInterrupted
		for i := range p.Phases {
			if p.Phases[i].Status == StatusRunning {
				p.Phases[i].Status = StatusInterrupted
			}
		}
		_ = e.save(ctx, &p)
	}
	return p, nil
}

func (e *Engine) load(ctx context.Context, planID types.ID) (Plan, error) {
	body, err := e.st.GetTeamworkPlan(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	return p, json.Unmarshal([]byte(body), &p)
}

// RemovePhase drops a work phase from a draft and re-validates the rest.
func (e *Engine) RemovePhase(ctx context.Context, planID types.ID, phaseID string) (Plan, error) {
	return e.edit(ctx, planID, func(d *Draft) error {
		for i, ph := range d.Phases {
			if ph.ID == phaseID {
				if len(d.Phases) == 1 {
					return errors.New("a plan needs at least one phase")
				}
				d.Phases = append(d.Phases[:i], d.Phases[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("no phase %s", phaseID)
	})
}

// RemoveAgent takes one agent off a draft's phase (never its last one).
func (e *Engine) RemoveAgent(ctx context.Context, planID types.ID, phaseID, character string) (Plan, error) {
	return e.edit(ctx, planID, func(d *Draft) error {
		for i, ph := range d.Phases {
			if ph.ID != phaseID {
				continue
			}
			if len(ph.Agents) <= 1 {
				return errors.New("a phase needs an agent")
			}
			for j, a := range ph.Agents {
				if a.Character == character {
					d.Phases[i].Agents = append(ph.Agents[:j], ph.Agents[j+1:]...)
					return nil
				}
			}
		}
		return fmt.Errorf("no agent %s on %s", character, phaseID)
	})
}

func (e *Engine) edit(ctx context.Context, planID types.ID, change func(*Draft) error) (Plan, error) {
	p, err := e.load(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != StatusDraft {
		return Plan{}, errors.New("only a plan that has not started can be edited")
	}
	d := toDraft(p)
	if err := change(&d); err != nil {
		return Plan{}, err
	}
	np, err := Validate(d, e.Cat)
	if err != nil {
		return Plan{}, err
	}
	np.ID, np.SessionID, np.Prompt, np.CreatedAt, np.UpdatedAt = p.ID, p.SessionID, p.Prompt, p.CreatedAt, time.Now().UTC()
	np.Notes = append(keepNotes(p.Notes), np.Notes...)
	if err := e.save(ctx, &np); err != nil {
		return Plan{}, err
	}
	e.publish(np)
	return np, nil
}

// keepNotes carries earlier trims over an edit; "solo" is recomputed.
func keepNotes(ns []Note) []Note {
	var out []Note
	for _, n := range ns {
		if n.Kind != "solo" {
			out = append(out, n)
		}
	}
	return out
}

func toDraft(p Plan) Draft {
	var d Draft
	d.Summary, d.Contract = p.Summary, p.Contract
	for _, ph := range p.Phases {
		if ph.Merge {
			continue
		}
		x := DraftPhase{ID: ph.ID, Title: ph.Title, Goal: ph.Goal, Files: ph.Files, DependsOn: ph.DependsOn}
		for _, a := range ph.Agents {
			x.Agents = append(x.Agents, DraftAgent{Character: a.Character, Why: a.Why})
		}
		d.Phases = append(d.Phases, x)
	}
	return d
}

// Discard deletes a plan that is not running, so the chat can start over.
func (e *Engine) Discard(ctx context.Context, planID types.ID) error {
	e.mu.Lock()
	_, live := e.running[planID]
	e.mu.Unlock()
	if live {
		return errors.New("cancel the plan first")
	}
	p, err := e.load(ctx, planID)
	if err != nil {
		return err
	}
	if err := e.st.DeleteTeamworkPlan(ctx, planID); err != nil {
		return err
	}
	for _, ph := range p.Phases {
		for _, a := range ph.Agents {
			if a.ChannelID == "" {
				continue
			}
			// the conductor's one-off subagents too
			if kids, err := e.sm.Children(ctx, a.ChannelID); err == nil {
				for _, k := range kids {
					_ = e.st.DeleteSession(ctx, k.ID)
					_ = e.st.DeleteSessionPersona(ctx, k.ID)
				}
			}
			_ = e.st.DeleteSession(ctx, a.ChannelID)
			_ = e.st.DeleteSessionPersona(ctx, a.ChannelID)
		}
	}
	e.publish(Plan{ID: planID, SessionID: p.SessionID})
	return nil
}

// Approve starts a draft: each agent gets its own channel, then the phases
// run wave by wave in the background.
func (e *Engine) Approve(ctx context.Context, planID types.ID) (Plan, error) {
	p, err := e.load(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != StatusDraft {
		return Plan{}, errors.New("this plan has already started")
	}
	if e.busy(p.SessionID) {
		return Plan{}, errors.New("a plan is already running in this chat")
	}
	home, err := e.sm.Get(ctx, p.SessionID)
	if err != nil {
		return Plan{}, err
	}
	// each part is an orchestra: its conductor's channel under the chat,
	// its members' channels under the conductor's. Every player plays with
	// the model picked for the chat; a channel left without one had none at
	// all when the chat's model is a connected account, not an agent's.
	var model types.ModelRef
	if e.ChatModel != nil {
		model = e.ChatModel(ctx, home.ID)
	}
	for i := range p.Phases {
		ph := &p.Phases[i]
		ph.Status = StatusPending
		var conductor types.Session
		for j := range ph.Agents {
			a := &ph.Agents[j]
			parent, space, title := home, types.SpaceTeamwork, ph.ID+" · "+e.name(a.Character)
			if j > 0 {
				parent, space, title = conductor, types.SpaceOrchestra, e.name(a.Character)
			}
			ch, err := e.sm.CreateChildIn(ctx, parent, space, title)
			if err != nil {
				return Plan{}, err
			}
			sp := types.SessionPersona{SessionID: ch.ID, CharacterID: a.Character, UpdatedAt: time.Now().UTC()}
			if ph.Merge {
				// the merge puts parts together and fixes what breaks: a
				// reviewer's eye, with the hands to change files
				sp.Features = mergeFeatures
			}
			if err := e.st.PutSessionPersona(ctx, sp); err != nil {
				return Plan{}, err
			}
			if model.Model != "" {
				if err := e.st.SetSessionModel(ctx, ch.ID, model); err != nil {
					return Plan{}, err
				}
			}
			a.ChannelID = ch.ID
			if j == 0 {
				conductor = ch
			}
		}
	}
	p.Status = StatusRunning
	if err := e.save(ctx, &p); err != nil {
		return Plan{}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r := &run{plan: &p, cancel: cancel}
	e.mu.Lock()
	e.running[p.ID] = r
	e.mu.Unlock()
	e.publish(p)
	go e.execute(runCtx, r, home, e.wsPath(ctx, home.WorkspaceID))
	return e.snapshot(r), nil
}

// Retry runs a stopped plan again from where it stopped: parts that are
// done keep their work and their reports, the rest — a failed part and
// every part waiting on it — play again in the same chats, so each
// orchestra sees what it did before.
func (e *Engine) Retry(ctx context.Context, planID types.ID) (Plan, error) {
	p, err := e.load(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	switch p.Status {
	case StatusFailed, StatusCanceled, StatusInterrupted, StatusRunning:
	default:
		return Plan{}, errors.New("only a plan that stopped can be tried again")
	}
	if e.busy(p.SessionID) {
		return Plan{}, errors.New("a plan is already running in this chat")
	}
	home, err := e.sm.Get(ctx, p.SessionID)
	if err != nil {
		return Plan{}, err
	}
	var model types.ModelRef
	if e.ChatModel != nil {
		model = e.ChatModel(ctx, home.ID)
	}
	for i := range p.Phases {
		if p.Phases[i].Status == StatusDone {
			continue
		}
		p.Phases[i].Status, p.Phases[i].Error = StatusPending, ""
		// a chat given a model since (or a plan from before players took
		// the chat's): no player is left without one
		for _, a := range p.Phases[i].Agents {
			if m, err := e.st.GetSessionModel(ctx, a.ChannelID); model.Model != "" && (err != nil || m.Model == "") {
				_ = e.st.SetSessionModel(ctx, a.ChannelID, model)
			}
		}
	}
	p.Status, p.Error = StatusRunning, ""
	if err := e.save(ctx, &p); err != nil {
		return Plan{}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r := &run{plan: &p, cancel: cancel}
	e.mu.Lock()
	e.running[p.ID] = r
	e.mu.Unlock()
	e.publish(p)
	go e.execute(runCtx, r, home, e.wsPath(ctx, home.WorkspaceID))
	return e.snapshot(r), nil
}

// Cancel stops a running plan; phases already done keep their work.
func (e *Engine) Cancel(ctx context.Context, planID types.ID) (Plan, error) {
	e.mu.Lock()
	r, ok := e.running[planID]
	e.mu.Unlock()
	if !ok {
		return e.load(ctx, planID)
	}
	r.cancel()
	return e.snapshot(r), nil
}

// Wait waits for a plan's run to end; false if it is still running at the deadline.
func (e *Engine) Wait(planID types.ID, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		_, live := e.running[planID]
		e.mu.Unlock()
		if !live {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func (e *Engine) busy(sessionID types.ID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.running {
		if r.plan.SessionID == sessionID {
			return true
		}
	}
	return false
}

// execute starts every part as soon as the parts it hangs on are done, so
// a short branch does not wait for a long one it has nothing to do with. A
// part whose input failed does not start; the plan fails once nothing more
// can run.
func (e *Engine) execute(ctx context.Context, r *run, home types.Session, wsPath string) {
	defer func() {
		e.mu.Lock()
		delete(e.running, r.plan.ID)
		e.mu.Unlock()
	}()
	done := make(chan int)
	started := map[int]bool{}
	live := 0
	for {
		var ready []int
		e.update(r, func(p *Plan) {
			status := map[string]Status{}
			for _, ph := range p.Phases {
				status[ph.ID] = ph.Status
			}
			for i, ph := range p.Phases {
				if started[i] || ph.Status != StatusPending {
					continue
				}
				ok := true
				for _, d := range ph.DependsOn {
					switch status[d] {
					case StatusDone:
					case StatusFailed, StatusCanceled:
						// its input will never come
						p.Phases[i].Status = StatusCanceled
						ok = false
					default:
						ok = false
					}
				}
				if ok && ctx.Err() == nil {
					ready = append(ready, i)
				}
			}
		})
		for _, i := range ready {
			started[i] = true
			live++
			go func(i int) {
				e.runPhase(ctx, r, i, home, wsPath)
				done <- i
			}(i)
		}
		if live == 0 {
			break
		}
		<-done
		live--
	}
	// finished work is checked before it is called done
	if ctx.Err() == nil && allDone(r) {
		e.verify(ctx, r, home, wsPath)
	}
	e.update(r, func(p *Plan) {
		p.Status = StatusDone
		p.Verifying = false
		for i := range p.Phases {
			switch p.Phases[i].Status {
			case StatusPending:
				p.Phases[i].Status = StatusCanceled
				p.Status = StatusFailed
			case StatusFailed, StatusCanceled:
				p.Status = StatusFailed
			}
		}
		if ctx.Err() != nil {
			p.Status = StatusCanceled
		}
	})
}

// mergeFeatures are the merge orchestra's permissions: it reads, writes,
// runs the build and tests, and checks before it says done.
var mergeFeatures = []string{"read", "write", "shell", "git", "codemap", "verify", "cite"}

// Turn budgets: splitting a part is a short look around; doing a piece or
// finishing the part is real work.
const (
	scoreTurns = 8
	workTurns  = 40
)

// runPhase plays a part's orchestra. A conductor alone does the part. With
// members, the engine — not a tool the model may or may not have — runs it
// in three movements, so it works the same with every model, agent CLIs
// included: the conductor splits the part into a piece per member, the
// members play their pieces side by side, each in its own chat, and the
// conductor takes their reports, fits the work together and finishes.
func (e *Engine) runPhase(ctx context.Context, r *run, i int, home types.Session, wsPath string) {
	var ph Phase
	var plan Plan
	e.update(r, func(p *Plan) {
		p.Phases[i].Status = StatusRunning
		ph, plan = p.Phases[i], *p
	})
	fail := func(err error) {
		e.update(r, func(p *Plan) {
			p.Phases[i].Status = StatusFailed
			if ctx.Err() != nil {
				p.Phases[i].Status = StatusCanceled
			} else {
				p.Phases[i].Error = err.Error()
			}
		})
	}
	play := func(a Agent, msg string, turns int) (agent.RunResult, error) {
		res, err := e.Run(ctx, agent.RunRequest{
			AgentID: home.AgentID, SessionID: a.ChannelID,
			WorkspaceID: home.WorkspaceID, Workspace: wsPath,
			UserMessage: msg, HistoryLimit: historyTail, MaxTurns: turns,
		})
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		return res, err
	}
	conductor, members := ph.Agents[0], ph.Agents[1:]
	var files []string
	var res agent.RunResult
	var err error
	if len(members) == 0 {
		if res, err = play(conductor, e.brief(plan, ph), workTurns); err != nil {
			fail(err)
			return
		}
	} else {
		// 1. the score: who plays what
		score, err := play(conductor, e.brief(plan, ph)+"\n\n"+e.scoreAsk(members), scoreTurns)
		if err != nil {
			fail(err)
			return
		}
		pieces := e.pieces(score.Assistant, ph)
		// 2. the members play their pieces side by side
		reports := make([]string, len(members))
		var wg sync.WaitGroup
		var errMu sync.Mutex
		var firstErr error
		for k, m := range members {
			wg.Add(1)
			go func(k int, m Agent) {
				defer wg.Done()
				out, err := play(m, e.memberBrief(plan, ph, m, pieces[k]), workTurns)
				if err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", e.name(m.Character), err)
					}
					errMu.Unlock()
					reports[k] = "(did not finish: " + err.Error() + ")"
					return
				}
				reports[k] = clipTail(out.Assistant, handoffClip)
				errMu.Lock()
				files = append(files, out.FilesEdited...)
				errMu.Unlock()
			}(k, m)
		}
		wg.Wait()
		if ctx.Err() != nil {
			fail(ctx.Err())
			return
		}
		// 3. the conductor brings it together, a member that failed included
		var b strings.Builder
		b.WriteString("Your orchestra has played. Their reports:\n")
		for k, m := range members {
			fmt.Fprintf(&b, "\n## %s\n%s\n", e.name(m.Character), reports[k])
		}
		if firstErr != nil {
			fmt.Fprintf(&b, "\nOne piece did not finish (%s): do it yourself.\n", firstErr)
		}
		b.WriteString("\nCheck their work in the files, fix what does not fit, finish the part. Then write the part's short report: what was changed (files) and anything left open.")
		if res, err = play(conductor, b.String(), workTurns); err != nil {
			fail(err)
			return
		}
	}
	report := strings.TrimSpace(res.Assistant)
	if fs := uniq(append(files, res.FilesEdited...)); len(fs) > 0 {
		report += "\nFiles: " + strings.Join(fs, ", ")
	}
	e.update(r, func(p *Plan) {
		p.Phases[i].Status = StatusDone
		p.Phases[i].Report = clipTail(report, reportClip)
	})
}

// scoreAsk is how the conductor is asked to split its part.
func (e *Engine) scoreAsk(members []Agent) string {
	var b strings.Builder
	b.WriteString("First split your part between your orchestra; do not change files yet. Your members:\n")
	for _, m := range members {
		line := "- " + e.name(m.Character)
		if m.Why != "" {
			line += ": " + m.Why
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(`Give each member one piece they can do on their own, side by side with the others, on files no other member changes.
The pieces are played at the same time, so no piece may wait for another's output: write into each task what the pieces
share (file names, element ids, class names, function names, data fields), so each member can build its piece against it.
Reply with JSON only:
{"pieces": [{"member": "name as listed", "task": "what to do, the files, how to check it"}]}`)
	return b.String()
}

// pieces reads the conductor's split, one task per member in member order.
// A member the conductor left out (or a split that is not JSON) gets what
// the plan said it is there for.
func (e *Engine) pieces(reply string, ph Phase) []string {
	members := ph.Agents[1:]
	out := make([]string, len(members))
	var score struct {
		Pieces []struct {
			Member string `json:"member"`
			Task   string `json:"task"`
		} `json:"pieces"`
	}
	if a, b := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); a >= 0 && b > a {
		_ = json.Unmarshal([]byte(reply[a:b+1]), &score)
	}
	for k, m := range members {
		name := strings.ToLower(e.name(m.Character))
		for _, p := range score.Pieces {
			who := strings.ToLower(strings.TrimSpace(p.Member))
			if who != "" && (who == name || who == strings.ToLower(m.Character) || strings.Contains(name, who)) && strings.TrimSpace(p.Task) != "" {
				out[k] = strings.TrimSpace(p.Task)
				break
			}
		}
		// pieces in order, when the names did not match
		if out[k] == "" && k < len(score.Pieces) && strings.TrimSpace(score.Pieces[k].Task) != "" {
			out[k] = strings.TrimSpace(score.Pieces[k].Task)
		}
		if out[k] == "" {
			out[k] = m.Why
		}
	}
	return out
}

// memberBrief is what a member is told: the part it plays in, its piece,
// and the files.
func (e *Engine) memberBrief(p Plan, ph Phase, m Agent, piece string) string {
	var b strings.Builder
	about := p.Summary
	if about == "" {
		about = clipRunes(p.Prompt, 300)
	}
	fmt.Fprintf(&b, "You play in %s's orchestra, on part %s %s of: %s\n", e.name(ph.Agents[0].Character), ph.ID, ph.Title, about)
	if p.Contract != "" {
		fmt.Fprintf(&b, "Shared decisions: %s\n", p.Contract)
	}
	fmt.Fprintf(&b, "\nYour piece: %s\n", piece)
	if len(ph.Files) > 0 {
		fmt.Fprintf(&b, "The part's files: %s. Other members work beside you on other pieces; change only what your piece needs.\n", strings.Join(ph.Files, ", "))
	}
	b.WriteString("The others are writing their pieces at this moment, so their files may not exist yet: do not wait for them and do not skip your piece because of it. Build it from the shared decisions and your task, and create your files.\n")
	b.WriteString("\nFinish with a short report for your conductor: what you changed (files) and anything left open.")
	return b.String()
}

// brief is all a conductor is told: its part, the shared contract, and what
// came in over the cables — the reports of the parts it hangs on, never
// their transcripts. A conductor with members is then asked for the split
// (scoreAsk).
func (e *Engine) brief(p Plan, ph Phase) string {
	var b strings.Builder
	about := p.Summary
	if about == "" {
		about = clipRunes(p.Prompt, 300)
	}
	if ph.Merge {
		fmt.Fprintf(&b, "You merge the work of several orchestras on: %s\n", about)
	} else {
		fmt.Fprintf(&b, "You conduct one of several orchestras working on: %s\n", about)
	}
	if p.Contract != "" {
		fmt.Fprintf(&b, "Shared decisions: %s\n", p.Contract)
	}
	byID := map[string]Phase{}
	for _, o := range p.Phases {
		byID[o.ID] = o
	}
	if len(ph.DependsOn) > 0 {
		b.WriteString("\nWork that reaches you from the parts before yours:\n")
		for _, d := range ph.DependsOn {
			o := byID[d]
			fmt.Fprintf(&b, "\n## %s %s\n%s\n", o.ID, o.Title, o.Report)
		}
	}
	if ph.Merge {
		b.WriteString("\nMake the parts work together: resolve conflicts, run the build and the tests, fix what breaks. Finish with a short report for the user: what was built, what you verified, anything left open.")
		return b.String()
	}
	fmt.Fprintf(&b, "\nYour part — %s %s: %s\n", ph.ID, ph.Title, ph.Goal)
	if len(ph.Files) > 0 {
		fmt.Fprintf(&b, "Change only: %s. Other orchestras are working on other parts in parallel; leave their files alone.\n", strings.Join(ph.Files, ", "))
	}
	if len(ph.Agents) > 1 {
		return b.String()
	}
	b.WriteString("\nYou play this part alone: do the work yourself.\n")
	b.WriteString("\nFinish with a short report: what you changed (files) and anything left open.")
	return b.String()
}

func (e *Engine) name(character string) string {
	if e.Name != nil {
		if n := e.Name(character); n != "" {
			return n
		}
	}
	return character
}

func (e *Engine) wsPath(ctx context.Context, ws types.ID) string {
	if e.WorkspacePath == nil || ws == "" {
		return ""
	}
	return e.WorkspacePath(ctx, ws)
}

// update changes a running plan under the lock, saves it and tells the UI.
func (e *Engine) update(r *run, change func(*Plan)) {
	e.mu.Lock()
	change(r.plan)
	r.plan.UpdatedAt = time.Now().UTC()
	snap := copyPlan(*r.plan)
	e.mu.Unlock()
	_ = e.save(context.Background(), &snap)
	e.publish(snap)
}

func (e *Engine) snapshot(r *run) Plan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return copyPlan(*r.plan)
}

func copyPlan(p Plan) Plan {
	b, _ := json.Marshal(p)
	var out Plan
	_ = json.Unmarshal(b, &out)
	return out
}

func (e *Engine) save(ctx context.Context, p *Plan) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return e.st.PutTeamworkPlan(ctx, p.ID, p.SessionID, string(b), p.CreatedAt)
}

func (e *Engine) publish(p Plan) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(types.Event{
		Type:    EventUpdated,
		Topic:   "session." + string(p.SessionID),
		Payload: map[string]any{"sessionId": string(p.SessionID), "planId": string(p.ID), "status": string(p.Status)},
	})
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func allDone(r *run) bool {
	for _, ph := range r.plan.Phases {
		if ph.Status != StatusDone {
			return false
		}
	}
	return len(r.plan.Phases) > 0
}

// verifyAsk asks for proof by commands, not an opinion.
const verifyAsk = `Before this work is called done, verify it yourself in the workspace, with commands. Do not reason about whether it works: run what proves it.
- Run the project's build, its tests and its linters, if it has them.
- A web page: serve the folder (python3 -m http.server on a free port, in the background), fetch the page and every file it references with curl (each must answer 200), check the ids and classes the scripts use exist in the HTML, and run each script with node --check; stop the server after.
- A command-line tool or library: run it on a real input and look at the output.
If a check fails, say so; do not fix anything in this step.
End your reply with one line of JSON only:
{"verified": true, "checks": ["command -> what it showed"], "problems": ["what is wrong"]}
verified is true only when every check you ran passed.`

// verify has the merge orchestra (or the only part's conductor) check the
// finished work by running it. A failed check gets one round of fixes and
// is checked again; what it finds is kept on the plan.
func (e *Engine) verify(ctx context.Context, r *run, home types.Session, wsPath string) {
	var channel types.ID
	e.mu.Lock()
	for _, ph := range r.plan.Phases {
		if len(ph.Agents) > 0 && (ph.Merge || channel == "") {
			channel = ph.Agents[0].ChannelID
		}
	}
	e.mu.Unlock()
	if channel == "" || e.Run == nil {
		return
	}
	e.update(r, func(p *Plan) { p.Verifying = true })
	ask := func(msg string) (Verification, bool) {
		res, err := e.Run(ctx, agent.RunRequest{
			AgentID: home.AgentID, SessionID: channel,
			WorkspaceID: home.WorkspaceID, Workspace: wsPath,
			UserMessage: msg, HistoryLimit: historyTail, MaxTurns: workTurns,
		})
		if err != nil {
			return Verification{}, false
		}
		return parseVerdict(res.Assistant)
	}
	v, ok := ask(verifyAsk)
	v.Rounds = 1
	if ok && !v.Verified && len(v.Problems) > 0 && ctx.Err() == nil {
		fix := "These checks failed:\n- " + strings.Join(v.Problems, "\n- ") +
			"\n\nFix them now. Then verify again the same way and end with the same one line of JSON."
		if v2, ok2 := ask(fix); ok2 {
			v, ok = v2, true
			v.Rounds = 2
		}
	}
	e.update(r, func(p *Plan) {
		p.Verifying = false
		if !ok {
			v = Verification{Problems: []string{"the check gave no verdict"}, Rounds: v.Rounds}
		}
		p.Verify = &v
	})
}

// parseVerdict reads the last JSON object with a "verified" key in a reply.
func parseVerdict(reply string) (Verification, bool) {
	i := strings.LastIndex(reply, `"verified"`)
	if i < 0 {
		return Verification{}, false
	}
	start := strings.LastIndex(reply[:i], "{")
	if start < 0 {
		return Verification{}, false
	}
	var v Verification
	if err := json.NewDecoder(strings.NewReader(reply[start:])).Decode(&v); err != nil {
		return Verification{}, false
	}
	return v, true
}
