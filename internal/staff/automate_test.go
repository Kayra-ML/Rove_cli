package staff

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

func callTool(t *testing.T, tl tool.Tool, sid types.ID, args string) tool.Result {
	t.Helper()
	res, err := tl.Call(context.Background(), tool.Context{SessionID: sid}, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func byName(e *Engine, name string) tool.Tool {
	for _, tl := range AutomationTools(e) {
		if tl.Name() == name {
			return tl
		}
	}
	return nil
}

// Set up in plain words from a chat: an agent watching it, a cable to
// another chat, a schedule — each listed, shown on the map, and removable.
func TestAutomationsFromAChat(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	qa := hire(t, st, "Ayşe", "Test uzmanı")
	ops := hire(t, st, "Ali", "DevOps")
	sm := session.New(st, nil)
	web, _ := sm.CreateIn(ctx, types.SpaceChat, "Web sitesi", "ag", "w1")
	app, _ := sm.CreateIn(ctx, types.SpaceChat, "Masaüstü uygulaması", "ag", "w2")

	// an agent watching this chat, named by title
	res := callTool(t, byName(e, AutoWatchName), web.ID, `{"agent":"test uzmanı","instruction":"Testleri gözden geçir","filter":"*.go","daily_limit":3}`)
	if res.IsError || !strings.Contains(res.Content, `Ayşe watches "Web sitesi"`) {
		t.Fatalf("watch = %+v", res)
	}
	ws, _ := e.Watches(ctx)
	if len(ws) != 1 || ws[0].SessionID != web.ID || ws[0].ProfileID != qa.ID || ws[0].Filter != "*.go" || ws[0].DailyLimit != 3 || !ws[0].Enabled {
		t.Fatalf("watches = %+v", ws)
	}
	// both cards are put on the map, to be seen
	nodes, _ := st.ListMapNodes(ctx)
	on := map[types.ID]bool{}
	for _, n := range nodes {
		on[n.SessionID] = true
	}
	if !on[web.ID] || !on["agent:"+qa.ID] {
		t.Fatalf("map = %+v", nodes)
	}

	// a cable to the other chat, by a part of its title; asking again finds it
	res = callTool(t, byName(e, AutoLinkName), web.ID, `{"chat":"masaüstü","direction":"to"}`)
	if res.IsError || !strings.Contains(res.Content, "Masaüstü uygulaması") {
		t.Fatalf("link = %+v", res)
	}
	links, _ := st.ListAllSessionLinks(ctx)
	if len(links) != 1 || links[0].Direction != types.LinkAToB || links[0].SessionB != app.ID {
		t.Fatalf("links = %+v", links)
	}
	if again := callTool(t, byName(e, AutoLinkName), web.ID, `{"chat":"masaüstü"}`); !strings.Contains(again.Content, "already") {
		t.Fatalf("second cable = %+v", again)
	}

	// a schedule, daily
	res = callTool(t, byName(e, AutoScheduleName), web.ID, `{"agent":"Ali","instruction":"Bağımlılık güncellemelerine bak","daily_at":"09:00"}`)
	if res.IsError || !strings.Contains(res.Content, "daily at 09:00") {
		t.Fatalf("schedule = %+v", res)
	}
	ss, _ := e.Schedules(ctx)
	if len(ss) != 1 || ss[0].ProfileID != ops.ID || ss[0].WorkspaceID != "w1" {
		t.Fatalf("schedules = %+v", ss)
	}
	if r := callTool(t, byName(e, AutoScheduleName), web.ID, `{"agent":"Ali","instruction":"x"}`); !r.IsError {
		t.Fatal("a schedule with no time was taken")
	}

	// all of it is listed, with ids to remove by
	list := callTool(t, byName(e, AutoListName), web.ID, `{}`).Content
	for _, want := range []string{"Ayşe — Test uzmanı", "[" + string(ws[0].ID) + "]", "[" + string(links[0].ID) + "]", "[" + string(ss[0].ID) + "]"} {
		if !strings.Contains(list, want) {
			t.Fatalf("list lacks %q:\n%s", want, list)
		}
	}
	for _, id := range []types.ID{ws[0].ID, links[0].ID, ss[0].ID} {
		if r := callTool(t, byName(e, AutoRemoveName), web.ID, `{"id":"[`+string(id)+`]"}`); r.IsError {
			t.Fatalf("remove %s = %+v", id, r)
		}
	}
	ws, _ = e.Watches(ctx)
	links, _ = st.ListAllSessionLinks(ctx)
	ss, _ = e.Schedules(ctx)
	if len(ws)+len(links)+len(ss) != 0 {
		t.Fatalf("left: %d watches, %d cables, %d schedules", len(ws), len(links), len(ss))
	}
	// an unknown agent is said plainly
	if r := callTool(t, byName(e, AutoWatchName), web.ID, `{"agent":"Mehmet","instruction":"x"}`); !r.IsError {
		t.Fatal("an unknown agent was taken")
	}
}

// Only a chat the user talks in sets up automations.
func TestOnlyUserChatsAutomate(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "")
	sm := session.New(st, nil)
	chat, _ := sm.CreateIn(ctx, types.SpaceChat, "c", "ag", "")
	office, _ := sm.CreateIn(ctx, types.SpaceOffice, "o", "ag", "")
	sub, _ := sm.CreateChildIn(ctx, chat, types.SpaceWorker, "Subagent")
	if !e.CanAutomate(ctx, chat.ID) || !e.CanAutomate(ctx, office.ID) {
		t.Fatal("a user's chat cannot automate")
	}
	if e.CanAutomate(ctx, sub.ID) {
		t.Fatal("a subagent can automate")
	}
	e.Run = func(context.Context, agent.RunRequest) (agent.RunResult, error) {
		return agent.RunResult{Done: true}, nil
	}
	task, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "iş"})
	if e.CanAutomate(ctx, task.SessionID) {
		t.Fatal("an agent's own task can automate")
	}
}

// A schedule runs at its slot, once, and not while the last run goes on.
func TestScheduleRunsOnTime(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "")
	var runs int32
	release := make(chan struct{})
	e.Run = func(context.Context, agent.RunRequest) (agent.RunResult, error) {
		atomic.AddInt32(&runs, 1)
		<-release
		return agent.RunResult{Assistant: "kontrol edildi", Done: true}, nil
	}
	day := time.Date(2026, 10, 3, 0, 0, 0, 0, time.Local)
	s, err := e.SaveSchedule(ctx, Schedule{ProfileID: ali.ID, Instruction: "Bağımlılıklara bak", DailyAt: "09:00"})
	if err != nil {
		t.Fatal(err)
	}
	s.LastRun = day.Add(8 * time.Hour) // made at 08:00
	_ = e.putSchedule(ctx, s)
	if n := e.Tick(ctx, day.Add(8*time.Hour+30*time.Minute)); n != 0 {
		t.Fatal("ran before its time")
	}
	if n := e.Tick(ctx, day.Add(9*time.Hour+time.Minute)); n != 1 {
		t.Fatal("did not run at its time")
	}
	if n := e.Tick(ctx, day.Add(9*time.Hour+2*time.Minute)); n != 0 {
		t.Fatal("ran twice the same day")
	}
	tasks, _ := e.Tasks(ctx, ali.ID, 0)
	if len(tasks) != 1 || tasks[0].ScheduleID != s.ID || tasks[0].Origin != "daily at 09:00" {
		t.Fatalf("tasks = %+v", tasks)
	}
	// the next day it is due again, but waits while yesterday's run goes on
	if n := e.Tick(ctx, day.Add(33*time.Hour)); n != 0 {
		t.Fatal("started while the last run still went on")
	}
	close(release)
	waitFor(t, func() bool { x, _ := e.Tasks(ctx, ali.ID, 0); return x[0].Status == StatusDone })
	if n := e.Tick(ctx, day.Add(33*time.Hour)); n != 1 {
		t.Fatal("did not run the next day")
	}
	// repeating: never more often than every 15 minutes
	r, _ := e.SaveSchedule(ctx, Schedule{ProfileID: ali.ID, Instruction: "x", EveryMinutes: 1})
	if r.EveryMinutes != minEveryMinutes {
		t.Fatalf("every = %d", r.EveryMinutes)
	}
}
