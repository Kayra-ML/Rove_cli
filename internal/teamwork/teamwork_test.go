package teamwork

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type cat map[string]bool

func (c cat) Has(id string) bool { return c[id] }

var catalog = cat{"frontend": true, "go-backend": true, "qa": true, "writer": true, "architect": true, "reviewer": true, "database": true, "uiux": true}

func draft(js string) Draft {
	var d Draft
	if err := json.Unmarshal([]byte(js), &d); err != nil {
		panic(err)
	}
	return d
}

func kinds(ns []Note) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Kind)
	}
	return out
}

// The screenshot case: three parallel parts with four agents each. It comes
// out as three phases, one agent each (plus one justified helper), a merge
// step, and notes saying what was trimmed.
func TestValidateKeepsAgentsFew(t *testing.T) {
	p, err := Validate(draft(`{"summary":"Google login","phases":[
		{"id":"a","title":"OAuth flow","files":["src/server/auth"],"agents":[{"character":"go-backend"},{"character":"qa","why":"token edge cases"},{"character":"architect"},{"character":"database"}]},
		{"id":"b","title":"Login button","files":["src/components/auth"],"agents":[{"character":"frontend"},{"character":"uiux"},{"character":"qa"},{"character":"writer"}]},
		{"id":"c","title":"Session guard + PRD","files":["src/middleware/auth","docs/prd"],"agents":[{"character":"go-backend"},{"character":"writer"}]}]}`), catalog)
	if err != nil {
		t.Fatal(err)
	}
	work := p.Phases[:3]
	if len(p.Phases) != 4 || !p.Phases[3].Merge || p.Phases[3].Agents[0].Character != MergeAgent {
		t.Fatalf("phases = %+v", p.Phases)
	}
	if len(work[0].Agents) != 2 || work[0].Agents[1].Why != "token edge cases" || len(work[1].Agents) != 1 || len(work[2].Agents) != 1 {
		t.Fatalf("agents = %+v / %+v / %+v", work[0].Agents, work[1].Agents, work[2].Agents)
	}
	if n := p.Agents(); n > MaxAgents || n != 5 {
		t.Fatalf("agents = %d", n)
	}
	for _, ph := range work {
		if ph.Wave != 1 {
			t.Fatalf("%s wave %d", ph.ID, ph.Wave)
		}
	}
	if p.Phases[3].Wave != 2 || strings.Join(p.Phases[3].DependsOn, ",") != "F1,F2,F3" {
		t.Fatalf("merge = %+v", p.Phases[3])
	}
	if !strings.Contains(strings.Join(kinds(p.Notes), ","), "tooManyAgents") {
		t.Fatalf("notes = %+v", p.Notes)
	}
}

// Parallel phases on the same files would collide: they become one.
func TestValidateFoldsOverlappingPhases(t *testing.T) {
	p, _ := Validate(draft(`{"phases":[
		{"id":"x","title":"Auth API","files":["src/auth"],"agents":[{"character":"go-backend"}]},
		{"id":"y","title":"Auth form","files":["./src/auth/login.tsx"],"agents":[{"character":"frontend"}]},
		{"id":"z","title":"Docs","files":["docs"],"agents":[{"character":"writer"}]}]}`), catalog)
	if len(p.Phases) != 3 { // two work phases + merge
		t.Fatalf("phases = %+v", p.Phases)
	}
	f1 := p.Phases[0]
	if f1.Title != "Auth API + Auth form" || strings.Join(f1.Files, ",") != "src/auth,src/auth/login.tsx" || len(f1.Agents) != 2 || f1.Agents[1].Why == "" {
		t.Fatalf("folded = %+v", f1)
	}
	if p.Notes[0].Kind != "merged" || p.Notes[0].Detail != "src/auth" {
		t.Fatalf("notes = %+v", p.Notes)
	}
}

func TestValidateCapsPhasesAndStaffsEmptyOnes(t *testing.T) {
	p, _ := Validate(draft(`{"phases":[
		{"title":"A","files":["a"]},{"title":"B","files":["b.go"]},{"title":"C","files":["c"]},
		{"title":"D","files":["d"]},{"title":"E","files":["e"]},{"title":"F","files":["f"],"agents":[{"character":"ghost"}]}]}`), catalog)
	work := 0
	for _, ph := range p.Phases {
		if !ph.Merge {
			work++
		}
		if len(ph.Agents) == 0 {
			t.Fatalf("%s has no agent", ph.ID)
		}
	}
	if work != MaxPhases {
		t.Fatalf("work phases = %d", work)
	}
	if p.Phases[1].Agents[0].Character != "go-backend" { // guessed from b.go
		t.Fatalf("guess = %+v", p.Phases[1].Agents)
	}
	k := strings.Join(kinds(p.Notes), ",")
	if !strings.Contains(k, "tooManyPhases") || !strings.Contains(k, "unknownAgent") {
		t.Fatalf("notes = %s", k)
	}
}

func TestValidateWavesAndCycles(t *testing.T) {
	p, _ := Validate(draft(`{"phases":[
		{"id":"db","title":"Schema","files":["db"],"agents":[{"character":"database"}]},
		{"id":"api","title":"API","files":["api"],"dependsOn":["db"],"agents":[{"character":"go-backend"}]},
		{"id":"ui","title":"UI","files":["ui"],"agents":[{"character":"frontend"}]}]}`), catalog)
	w := map[string]int{}
	for _, ph := range p.Phases {
		w[ph.ID] = ph.Wave
	}
	if w["F1"] != 1 || w["F2"] != 2 || w["F3"] != 1 || w["M"] != 3 {
		t.Fatalf("waves = %v", w)
	}
	c, _ := Validate(draft(`{"phases":[
		{"id":"a","title":"A","files":["a"],"dependsOn":["b"],"agents":[{"character":"qa"}]},
		{"id":"b","title":"B","files":["b"],"dependsOn":["a"],"agents":[{"character":"qa"}]}]}`), catalog)
	if !strings.Contains(strings.Join(kinds(c.Notes), ","), "cycle") || c.Phases[0].Wave == 0 || c.Phases[1].Wave == 0 {
		t.Fatalf("cycle plan = %+v", c)
	}
}

func TestValidateSmallTaskIsOnePhase(t *testing.T) {
	p, _ := Validate(draft(`{"phases":[{"title":"Fix typo","files":["README.md"],"agents":[{"character":"writer"}]}]}`), catalog)
	if len(p.Phases) != 1 || p.Phases[0].Merge || kinds(p.Notes)[0] != "solo" {
		t.Fatalf("plan = %+v", p)
	}
	if _, err := Validate(Draft{}, catalog); err == nil {
		t.Fatal("empty plan accepted")
	}
}

func TestValidateTotalAgentCap(t *testing.T) {
	p, _ := Validate(draft(`{"phases":[
		{"title":"A","files":["a"],"agents":[{"character":"frontend"},{"character":"qa","why":"x"},{"character":"uiux","why":"y"},{"character":"writer","why":"z"}]},
		{"title":"B","files":["b"],"agents":[{"character":"go-backend"},{"character":"qa","why":"x"},{"character":"database","why":"y"},{"character":"writer","why":"z"}]},
		{"title":"C","files":["c"],"agents":[{"character":"database"},{"character":"qa","why":"x"},{"character":"go-backend","why":"y"},{"character":"writer","why":"z"}]},
		{"title":"D","files":["d"],"agents":[{"character":"writer"},{"character":"qa","why":"x"},{"character":"frontend","why":"y"},{"character":"uiux","why":"z"}]},
		{"title":"E","files":["e"],"agents":[{"character":"architect"},{"character":"qa","why":"x"},{"character":"frontend","why":"y"},{"character":"uiux","why":"z"}]}]}`), catalog)
	if n := p.Agents(); n != MaxAgents {
		t.Fatalf("agents = %d", n)
	}
	// members are taken evenly: no orchestra loses its conductor, none is
	// left with two more players than another
	lo, hi := 99, 0
	for _, ph := range p.Phases {
		if ph.Merge {
			continue
		}
		lo, hi = min(lo, len(ph.Agents)), max(hi, len(ph.Agents))
	}
	if lo < 2 || hi-lo > 1 {
		t.Fatalf("orchestras %d..%d players", lo, hi)
	}
}
func TestParseDraftAndProjectTree(t *testing.T) {
	d, err := ParseDraft("Here is the plan:\n```json\n{\"summary\":\"s\",\"phases\":[{\"title\":\"A\"}]}\n```\nGood luck")
	if err != nil || d.Summary != "s" || len(d.Phases) != 1 {
		t.Fatalf("%v %+v", err, d)
	}
	if _, err := ParseDraft("ok"); err == nil {
		t.Fatal("prose accepted as a plan")
	}
	if _, err := ParseDraft(`{"phases":[]}`); err == nil {
		t.Fatal("empty plan accepted")
	}
	root := t.TempDir()
	for _, f := range []string{"src/app/a.ts", "src/app/deep/b.ts", "node_modules/x/y.js", "README.md"} {
		p := filepath.Join(root, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o644)
	}
	tree := ProjectTree(root)
	if !strings.Contains(tree, "src/app/") || !strings.Contains(tree, "README.md") || strings.Contains(tree, "node_modules") || strings.Contains(tree, "deep/b.ts") {
		t.Fatalf("tree = %q", tree)
	}
	if !strings.Contains(PlannerPrompt([]CharacterLine{{"qa", "QA", "tests"}}), "- qa — QA — tests") {
		t.Fatal("characters missing from the planner prompt")
	}
}

// --- engine ---

type harness struct {
	e    *Engine
	st   *store.Store
	sm   *session.Manager
	home types.Session
	mu   sync.Mutex
	reqs []agent.RunRequest
}

func newHarness(t *testing.T, plannerReply string, run Runner) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bus := eventbus.New()
	sm := session.New(st, bus)
	home, _ := sm.CreateIn(context.Background(), types.SpaceChat, "", "ag", "")
	h := &harness{st: st, sm: sm, home: home}
	h.e = New(st, sm, bus)
	h.e.Cat = catalog
	h.e.Ask = func(context.Context, types.ID, types.ModelRef, string, string) (string, types.ModelRef, error) {
		return plannerReply, types.ModelRef{}, nil
	}
	h.e.Run = func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
		h.mu.Lock()
		h.reqs = append(h.reqs, req)
		h.mu.Unlock()
		return run(ctx, req)
	}
	return h
}

const twoParts = `{"summary":"Google login","contract":"POST /login returns {token}","phases":[
	{"id":"api","title":"Login API","files":["server/auth"],"agents":[{"character":"go-backend"}]},
	{"id":"ui","title":"Login button","files":["web/login"],"agents":[{"character":"frontend"}]}]}`

func TestEngineRunsApprovedPlanInWavesThenMerges(t *testing.T) {
	var inflight, peak int32
	h := newHarness(t, twoParts, func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
		n := atomic.AddInt32(&inflight, 1)
		for p := atomic.LoadInt32(&peak); n > p && !atomic.CompareAndSwapInt32(&peak, p, n); p = atomic.LoadInt32(&peak) {
		}
		time.Sleep(60 * time.Millisecond)
		atomic.AddInt32(&inflight, -1)
		return agent.RunResult{Assistant: "did " + strings.SplitN(req.UserMessage, "\n", 4)[2], FilesEdited: []string{"x"}}, nil
	})
	ctx := context.Background()
	p, err := h.e.Plan(ctx, h.home.ID, "Google ile giriş ekle")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusDraft || len(p.Phases) != 3 || len(h.reqs) != 0 {
		t.Fatalf("draft = %+v (runs %d)", p, len(h.reqs))
	}
	// nothing runs before approval
	time.Sleep(30 * time.Millisecond)
	if len(h.reqs) != 0 {
		t.Fatal("ran before approval")
	}
	p, err = h.e.Approve(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !h.e.Wait(p.ID, 5*time.Second) {
		t.Fatal("plan did not finish")
	}
	got, _ := h.e.Get(ctx, h.home.ID)
	if got.Status != StatusDone {
		t.Fatalf("status = %s (%+v)", got.Status, got.Phases)
	}
	if peak != 2 {
		t.Fatalf("parallel phases peak = %d", peak)
	}
	// two parts, the merge, and the check of the finished work
	if len(h.reqs) != 4 || !strings.Contains(h.reqs[3].UserMessage, "verify it yourself") {
		t.Fatalf("runs = %d", len(h.reqs))
	}
	merge := h.reqs[2]
	if !strings.Contains(merge.UserMessage, "## F1 Login API") || !strings.Contains(merge.UserMessage, "## F2 Login button") || !strings.Contains(merge.UserMessage, "Files: x") {
		t.Fatalf("merge brief = %q", merge.UserMessage)
	}
	for _, r := range h.reqs[:2] {
		if !strings.Contains(r.UserMessage, "Shared decisions: POST /login returns {token}") || !strings.Contains(r.UserMessage, "Change only:") || r.HistoryLimit != historyTail {
			t.Fatalf("phase brief = %q", r.UserMessage)
		}
	}
	// each agent worked in its own channel, a Teamwork child of the chat
	seen := map[types.ID]bool{}
	for _, ph := range got.Phases {
		for _, a := range ph.Agents {
			ch, err := h.st.GetSession(ctx, a.ChannelID)
			if err != nil || ch.ParentID != h.home.ID || ch.Space != types.SpaceTeamwork || seen[ch.ID] {
				t.Fatalf("channel %+v %v", ch, err)
			}
			seen[ch.ID] = true
			sp, _ := h.st.GetSessionPersona(ctx, ch.ID)
			if sp.CharacterID != a.Character {
				t.Fatalf("channel persona = %+v", sp)
			}
		}
		if ph.Status != StatusDone || ph.Report == "" {
			t.Fatalf("phase %+v", ph)
		}
	}
}

func TestEngineCancelAndFailure(t *testing.T) {
	block := make(chan struct{})
	h := newHarness(t, twoParts, func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
		select {
		case <-ctx.Done():
			return agent.RunResult{}, ctx.Err()
		case <-block:
			return agent.RunResult{Assistant: "done"}, nil
		}
	})
	ctx := context.Background()
	p, _ := h.e.Plan(ctx, h.home.ID, "x")
	p, _ = h.e.Approve(ctx, p.ID)
	if _, err := h.e.Plan(ctx, h.home.ID, "another"); err == nil {
		t.Fatal("planned over a running plan")
	}
	_, _ = h.e.Cancel(ctx, p.ID)
	if !h.e.Wait(p.ID, 5*time.Second) {
		t.Fatal("cancel did not stop the plan")
	}
	got, _ := h.e.Get(ctx, h.home.ID)
	if got.Status != StatusCanceled || got.Phases[2].Status != StatusCanceled {
		t.Fatalf("after cancel = %+v", got)
	}

	// a failing phase fails the plan and the merge never runs
	h2 := newHarness(t, twoParts, func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
		if strings.Contains(req.UserMessage, "Login button") && !strings.Contains(req.UserMessage, "You merge") {
			return agent.RunResult{}, errors.New("model down")
		}
		return agent.RunResult{Assistant: "ok"}, nil
	})
	p2, _ := h2.e.Plan(ctx, h2.home.ID, "x")
	p2, _ = h2.e.Approve(ctx, p2.ID)
	h2.e.Wait(p2.ID, 5*time.Second)
	g2, _ := h2.e.Get(ctx, h2.home.ID)
	if g2.Status != StatusFailed || g2.Phases[1].Error != "model down" || g2.Phases[2].Status != StatusCanceled {
		t.Fatalf("after failure = %+v", g2)
	}
	for _, r := range h2.reqs {
		if strings.Contains(r.UserMessage, "You merge") {
			t.Fatal("merge ran after a failed phase")
		}
	}
}

func TestEngineEditsDraftsFallsBackAndReportsInterrupted(t *testing.T) {
	h := newHarness(t, twoParts, func(context.Context, agent.RunRequest) (agent.RunResult, error) { return agent.RunResult{}, nil })
	ctx := context.Background()
	p, _ := h.e.Plan(ctx, h.home.ID, "x")
	p, err := h.e.RemovePhase(ctx, p.ID, "F2")
	if err != nil || len(p.Phases) != 1 || kinds(p.Notes)[len(p.Notes)-1] != "solo" {
		t.Fatalf("after removing a phase: %v %+v", err, p)
	}
	if _, err := h.e.RemovePhase(ctx, p.ID, "F1"); err == nil {
		t.Fatal("removed the last phase")
	}
	if _, err := h.e.RemoveAgent(ctx, p.ID, "F1", "go-backend"); err == nil {
		t.Fatal("removed a phase's only agent")
	}
	// asking again replaces the unapproved draft instead of piling up plans
	p3, err := h.e.Plan(ctx, h.home.ID, "y")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.e.load(ctx, p.ID); err == nil {
		t.Fatal("old draft kept after a new request")
	}
	if g, _ := h.e.Get(ctx, h.home.ID); g.ID != p3.ID {
		t.Fatalf("current plan = %s, want %s", g.ID, p3.ID)
	}

	// a planner that answers in prose still yields a runnable plan
	h2 := newHarness(t, "ok", func(context.Context, agent.RunRequest) (agent.RunResult, error) { return agent.RunResult{}, nil })
	p2, err := h2.e.Plan(ctx, h2.home.ID, "README yaz")
	if err != nil || len(p2.Phases) != 1 || p2.Notes[0].Kind != "plannerFallback" || p2.Phases[0].Goal != "README yaz" {
		t.Fatalf("fallback = %v %+v", err, p2)
	}
	// a phase without files is sent as "files": [], not null
	if b, _ := json.Marshal(p2); !strings.Contains(string(b), `"files":[]`) || strings.Contains(string(b), `"files":null`) {
		t.Fatalf("json = %s", b)
	}

	// a plan the daemon left running reads as interrupted
	p2.Status = StatusRunning
	p2.Phases[0].Status = StatusRunning
	_ = h2.e.save(ctx, &p2)
	g, _ := h2.e.Get(ctx, h2.home.ID)
	if g.Status != StatusInterrupted || g.Phases[0].Status != StatusInterrupted {
		t.Fatalf("interrupted = %+v", g)
	}
	if err := h2.e.Discard(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h2.e.Get(ctx, h2.home.ID); err == nil {
		t.Fatal("plan still there after discard")
	}
}

// Each part is an orchestra: members sit under the conductor's channel.
// The engine plays it in three movements — the conductor splits the part,
// the members play their pieces in their own chats, the conductor finishes
// with their reports. Parts start when what they hang on is done — not when
// the whole wave is — and get its report over the cable.
func TestEngineOrchestrasAndCables(t *testing.T) {
	const plan = `{"summary":"shop","phases":[
		{"id":"db","title":"Schema","files":["db"],"agents":[{"character":"database"},{"character":"qa","why":"migration tests"}]},
		{"id":"ui","title":"Shop UI","files":["ui"],"agents":[{"character":"frontend"}]},
		{"id":"api","title":"Shop API","files":["api"],"dependsOn":["db"],"agents":[{"character":"go-backend"}]}]}`
	var mu sync.Mutex
	var order []string
	done := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	h := newHarness(t, plan, func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
		m := req.UserMessage
		switch {
		case strings.Contains(m, `"pieces"`):
			return agent.RunResult{Assistant: "```json\n{\"pieces\":[{\"member\":\"qa\",\"task\":\"write migration tests in db/\"}]}\n```"}, nil
		case strings.Contains(m, "You play in"):
			return agent.RunResult{Assistant: "tests written", FilesEdited: []string{"db/m_test.sql"}}, nil
		case strings.Contains(m, "Your orchestra has played"):
			done("Schema")
			return agent.RunResult{Assistant: "Schema ready", FilesEdited: []string{"db/schema.sql"}}, nil
		case strings.Contains(m, "Shop UI:"):
			time.Sleep(150 * time.Millisecond)
			done("Shop UI")
		case strings.Contains(m, "Shop API:"):
			done("Shop API")
		case strings.Contains(m, "You merge"):
			done("merge")
		}
		return agent.RunResult{Assistant: "ok"}, nil
	})
	ctx := context.Background()
	chatModel := types.ModelRef{Provider: "Codex", Model: "gpt-6-luna"}
	h.e.ChatModel = func(context.Context, types.ID) types.ModelRef { return chatModel }
	p, err := h.e.Plan(ctx, h.home.ID, "shop")
	if err != nil {
		t.Fatal(err)
	}
	p, _ = h.e.Approve(ctx, p.ID)
	if !h.e.Wait(p.ID, 5*time.Second) {
		t.Fatal("plan did not finish")
	}
	got, _ := h.e.Get(ctx, h.home.ID)
	if got.Status != StatusDone {
		t.Fatalf("status %s %+v", got.Status, got.Phases)
	}
	// the API part, hanging only on the schema, finished before the slow UI
	if strings.Join(order, ",") != "Schema,Shop API,Shop UI,merge" {
		t.Fatalf("order = %v", order)
	}
	// the schema orchestra: score + member + finish; one run for each other
	// part; then the check of the finished work
	if len(h.reqs) != 7 {
		t.Fatalf("runs = %d", len(h.reqs))
	}
	db := got.Phases[0]
	cond, _ := h.st.GetSession(ctx, db.Agents[0].ChannelID)
	member, _ := h.st.GetSession(ctx, db.Agents[1].ChannelID)
	if cond.ParentID != h.home.ID || cond.Space != types.SpaceTeamwork || member.ParentID != cond.ID || member.Space != types.SpaceOrchestra {
		t.Fatalf("conductor %+v member %+v", cond, member)
	}
	// every player plays with the chat's model
	for _, id := range []types.ID{cond.ID, member.ID} {
		if m, _ := h.st.GetSessionModel(ctx, id); m != chatModel {
			t.Fatalf("channel %s model = %+v", id, m)
		}
	}
	var score, play, finish, api *agent.RunRequest
	for i := range h.reqs {
		r := &h.reqs[i]
		switch m := r.UserMessage; {
		case strings.Contains(m, `"pieces"`):
			score = r
		case strings.Contains(m, "You play in"):
			play = r
		case strings.Contains(m, "Your orchestra has played"):
			finish = r
		case strings.Contains(m, "Shop API:"):
			api = r
		}
	}
	if score == nil || score.SessionID != cond.ID || !strings.Contains(score.UserMessage, "- qa: migration tests") {
		t.Fatalf("score = %+v", score)
	}
	if play == nil || play.SessionID != member.ID || !strings.Contains(play.UserMessage, "Your piece: write migration tests in db/") {
		t.Fatalf("member run = %+v", play)
	}
	if finish == nil || finish.SessionID != cond.ID || !strings.Contains(finish.UserMessage, "## qa\ntests written") {
		t.Fatalf("finish = %+v", finish)
	}
	if api == nil || !strings.Contains(api.UserMessage, "## F1 Schema\nSchema ready") || !strings.Contains(api.UserMessage, "alone") {
		t.Fatalf("cable brief = %+v", api)
	}
	if !strings.Contains(db.Report, "db/m_test.sql") || !strings.Contains(db.Report, "db/schema.sql") {
		t.Fatalf("part report = %q", db.Report)
	}
	// discarding a finished plan removes the members' channels too
	if err := h.e.Discard(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.GetSession(ctx, member.ID); err == nil {
		t.Fatal("member channel left behind")
	}
}

// A split that is not JSON still gives every member something: what the
// plan says it is there for.
func TestPiecesFallBack(t *testing.T) {
	e := &Engine{}
	ph := Phase{Agents: []Agent{{Character: "lead"}, {Character: "qa", Why: "edge cases"}, {Character: "writer", Why: "docs"}}}
	got := e.pieces(`{"pieces":[{"member":"Writer","task":"README"}]}`, ph)
	if got[0] != "README" && got[1] != "README" {
		t.Fatalf("pieces = %q", got)
	}
	if got := e.pieces("I will just do it", ph); got[0] != "edge cases" || got[1] != "docs" {
		t.Fatalf("fallback = %q", got)
	}
}

// A stopped plan is tried again from where it stopped: done parts keep
// their work and are not played again.
func TestEngineRetry(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	var apiRuns atomic.Int32
	h := newHarness(t, twoParts, func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
		if strings.Contains(req.UserMessage, "Login API:") {
			apiRuns.Add(1)
		}
		if down.Load() && strings.Contains(req.UserMessage, "Login button:") {
			return agent.RunResult{}, errors.New("model down")
		}
		return agent.RunResult{Assistant: "ok"}, nil
	})
	ctx := context.Background()
	p, _ := h.e.Plan(ctx, h.home.ID, "x")
	if _, err := h.e.Retry(ctx, p.ID); err == nil {
		t.Fatal("retried a draft")
	}
	p, _ = h.e.Approve(ctx, p.ID)
	h.e.Wait(p.ID, 5*time.Second)
	got, _ := h.e.Get(ctx, h.home.ID)
	if got.Status != StatusFailed {
		t.Fatalf("first run = %s", got.Status)
	}
	down.Store(false)
	if _, err := h.e.Retry(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if !h.e.Wait(p.ID, 5*time.Second) {
		t.Fatal("retry did not finish")
	}
	got, _ = h.e.Get(ctx, h.home.ID)
	if got.Status != StatusDone || got.Phases[1].Error != "" {
		t.Fatalf("after retry = %+v", got)
	}
	if apiRuns.Load() != 1 {
		t.Fatalf("a done part played again: %d runs", apiRuns.Load())
	}
}

func TestClipTailKeepsTheResult(t *testing.T) {
	long := strings.Repeat("Önce dosyalara bakıyorum. ", 80) + "\n\nSonuç: app.js eklendi, testler geçti."
	got := clipTail(long, 200)
	if !strings.HasSuffix(got, "Sonuç: app.js eklendi, testler geçti.") || len([]rune(got)) > 200 {
		t.Fatalf("clipTail = %q", got)
	}
	if clipTail("kısa", 200) != "kısa" {
		t.Fatal("short report changed")
	}
}

// Finished work is checked by running it. A failed check gets one round of
// fixes and is checked again; a reply without a verdict is not a pass.
func TestEngineVerifiesBeforeDone(t *testing.T) {
	var checks atomic.Int32
	h := newHarness(t, twoParts, func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
		switch m := req.UserMessage; {
		case strings.Contains(m, "verify it yourself"):
			checks.Add(1)
			return agent.RunResult{Assistant: "curl index.html -> 404\n" + `{"verified": false, "checks": ["curl / -> 404"], "problems": ["index.html missing"]}`}, nil
		case strings.Contains(m, "These checks failed"):
			if !strings.Contains(m, "index.html missing") {
				t.Errorf("fix ask = %q", m)
			}
			return agent.RunResult{Assistant: "fixed.\n" + `{"verified": true, "checks": ["curl / -> 200", "node --check app.js -> ok"], "problems": []}`}, nil
		}
		return agent.RunResult{Assistant: "ok"}, nil
	})
	ctx := context.Background()
	p, _ := h.e.Plan(ctx, h.home.ID, "x")
	p, _ = h.e.Approve(ctx, p.ID)
	h.e.Wait(p.ID, 5*time.Second)
	got, _ := h.e.Get(ctx, h.home.ID)
	if got.Verify == nil || !got.Verify.Verified || got.Verify.Rounds != 2 || len(got.Verify.Checks) != 2 || got.Verifying {
		t.Fatalf("verify = %+v", got.Verify)
	}
	// the merge may fix what it finds
	merge := got.Phases[len(got.Phases)-1]
	sp, _ := h.st.GetSessionPersona(ctx, merge.Agents[0].ChannelID)
	if !strings.Contains(strings.Join(sp.Features, ","), "write") {
		t.Fatalf("merge features = %v", sp.Features)
	}
	if checks.Load() != 1 {
		t.Fatalf("checks = %d", checks.Load())
	}

	if v, ok := parseVerdict("no json here"); ok || v.Verified {
		t.Fatal("a reply without a verdict passed")
	}
}
