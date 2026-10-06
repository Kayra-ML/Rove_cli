package staff

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

func setup(t *testing.T) (*Engine, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := New(st, session.New(st, nil), nil)
	t.Cleanup(e.Close)
	e.DefaultAgent = func(context.Context) (types.ID, error) { return "ag", nil }
	return e, st
}

func hire(t *testing.T, st *store.Store, name, title string) types.AgentProfile {
	t.Helper()
	p, err := st.UpsertAgentProfile(context.Background(), types.AgentProfile{ID: id.NewID(), Name: name, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

// A task runs in the background in a conversation of its own, with the
// agent as its persona; the board shows it working, then a report to read,
// then idle once read.
func TestAssignRunsInTheBackgroundAndReports(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "Backend geliştirici")
	release := make(chan struct{})
	var got agent.RunRequest
	var mu sync.Mutex
	e.Run = func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		mu.Lock()
		got = req
		mu.Unlock()
		<-release
		return agent.RunResult{Assistant: "Added POST /login; go test ./... passes.", Done: true}, nil
	}
	task, err := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "Add a login endpoint\nwith tests"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Add a login endpoint" || task.Status != StatusRunning || task.SessionID == "" {
		t.Fatalf("task = %+v", task)
	}
	if sp, _ := st.GetSessionPersona(ctx, task.SessionID); sp.ProfileID != ali.ID {
		t.Fatalf("the task's chat is not Ali's: %+v", sp)
	}
	if s, _ := st.GetSession(ctx, task.SessionID); s.Space != types.SpaceOffice {
		t.Fatalf("space = %q", s.Space)
	}
	ms, _ := e.Members(ctx)
	if len(ms) != 1 || ms[0].State != "working" || ms[0].Now != "Add a login endpoint" {
		t.Fatalf("members = %+v", ms)
	}
	close(release)
	waitFor(t, func() bool { x, _ := e.Get(ctx, task.ID); return x.Status == StatusDone })
	mu.Lock()
	if !strings.HasPrefix(got.UserMessage, "Task from the user:\nAdd a login endpoint") || got.MaxTurns != taskTurns ||
		!strings.Contains(got.SystemExtra, "short report") || got.AgentID != "ag" {
		t.Fatalf("run = %+v", got)
	}
	mu.Unlock()
	ms, _ = e.Members(ctx)
	if ms[0].State != "report" || ms[0].Tasks[0].Report != "Added POST /login; go test ./... passes." {
		t.Fatalf("after = %+v", ms[0])
	}
	if _, err := e.Seen(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	ms, _ = e.Members(ctx)
	if ms[0].State != "idle" {
		t.Fatalf("after reading = %s", ms[0].State)
	}
}

func TestStopAndRecover(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "")
	e.Run = func(ctx context.Context, _ agent.RunRequest) (agent.RunResult, error) {
		<-ctx.Done()
		return agent.RunResult{}, ctx.Err()
	}
	var canceled types.ID
	e.Cancel = func(id types.ID) { canceled = id }
	task, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "long job"})
	stopped, err := e.Stop(ctx, task.ID)
	if err != nil || stopped.Status != StatusStopped || canceled != task.SessionID {
		t.Fatalf("stop = %+v %v (canceled %s)", stopped, err, canceled)
	}

	// a task a stopped daemon left running is closed on the next start
	left := Task{ID: id.NewID(), ProfileID: ali.ID, SessionID: "s", Title: "x", Status: StatusRunning, CreatedAt: time.Now().UTC()}
	b, _ := json.Marshal(left)
	_ = st.PutStaffTask(ctx, left.ID, left.ProfileID, left.SessionID, string(b), left.CreatedAt)
	e.Recover(ctx)
	if got, _ := e.Get(ctx, left.ID); got.Status != StatusStopped {
		t.Fatalf("recovered = %+v", got)
	}
}

// An agent knows who it is, keeps notes across conversations, and can ask a
// colleague for a part of the job — one level deep: the colleague does that
// part itself.
func TestNotesColleaguesAndAsking(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "Backend geliştirici")
	ayse := hire(t, st, "Ayşe", "Tasarımcı")
	sm := session.New(st, nil)
	chat, _ := sm.CreateIn(ctx, types.SpaceOffice, "c", "ag", "")
	_ = st.PutSessionPersona(ctx, types.SessionPersona{SessionID: chat.ID, ProfileID: ali.ID})

	if _, err := (NoteTool{E: e}).Call(ctx, tool.Context{SessionID: chat.ID}, json.RawMessage(`{"note":"Kullanıcı Go 1.23 kullanıyor"}`)); err != nil {
		t.Fatal(err)
	}
	brief := e.Brief(ctx, chat.ID)
	for _, want := range []string{"You are Ali, Backend geliştirici", "- Kullanıcı Go 1.23 kullanıyor", "- Ayşe — Tasarımcı", AskName} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	if !e.CanAsk(ctx, chat.ID) {
		t.Fatal("Ali cannot ask Ayşe")
	}

	var asked agent.RunRequest
	e.Run = func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		asked = req
		return agent.RunResult{Assistant: "Login ekranı çizildi.", Done: true}, nil
	}
	res, err := (AskTool{E: e}).Call(ctx, tool.Context{SessionID: chat.ID}, json.RawMessage(`{"colleague":"tasarımcı","task":"Login ekranını çiz","context":"mobil öncelikli"}`))
	if err != nil || res.IsError || res.Content != "Report from Ayşe (done):\nLogin ekranı çizildi." {
		t.Fatalf("ask = %+v %v", res, err)
	}
	if !strings.HasPrefix(asked.UserMessage, "Task from your colleague Ali:\nLogin ekranını çiz\n\nContext:\nmobil öncelikli") {
		t.Fatalf("Ayşe was told %q", asked.UserMessage)
	}
	tasks, _ := e.Tasks(ctx, ayse.ID, 0)
	if len(tasks) != 1 || tasks[0].From != ali.ID || !tasks[0].Seen {
		t.Fatalf("Ayşe's tasks = %+v", tasks)
	}
	// Ayşe's chat for Ali's request: she does it herself, no asking on
	if e.CanAsk(ctx, tasks[0].SessionID) || strings.Contains(e.Brief(ctx, tasks[0].SessionID), "Your colleagues") {
		t.Fatal("a colleague's request may be passed on")
	}
	if r, _ := (AskTool{E: e}).Call(ctx, tool.Context{SessionID: tasks[0].SessionID}, json.RawMessage(`{"colleague":"Ali","task":"x"}`)); !r.IsError {
		t.Fatal("asking from a colleague's request went through")
	}
	// nobody by that name
	if r, _ := (AskTool{E: e}).Call(ctx, tool.Context{SessionID: chat.ID}, json.RawMessage(`{"colleague":"Mehmet","task":"x"}`)); !r.IsError || !strings.Contains(r.Content, "Ayşe") {
		t.Fatalf("unknown colleague = %+v", r)
	}
	// a chat without an Office agent has neither
	other, _ := sm.CreateIn(ctx, types.SpaceChat, "c2", "ag", "")
	if e.Brief(ctx, other.ID) != "" || e.CanAsk(ctx, other.ID) {
		t.Fatal("a Code chat got the Agent space's brief")
	}
}

// An agent with no tasks and no notes yet is sent with empty lists, never
// null: the app reads their length, and a null once blanked the whole app.
func TestMembersNeverSendNullLists(t *testing.T) {
	e, st := setup(t)
	hire(t, st, "Yeni", "")
	ms, err := e.Members(context.Background())
	if err != nil || len(ms) != 1 {
		t.Fatalf("members = %+v, %v", ms, err)
	}
	b, _ := json.Marshal(ms[0])
	if strings.Contains(string(b), `"tasks":null`) || strings.Contains(string(b), `"notes":null`) {
		t.Fatalf("null list in %s", b)
	}
}
