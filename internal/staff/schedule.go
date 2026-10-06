package staff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

// Schedule gives an agent a task on a timer: every so many minutes, or
// every day at a time. Each run is an ordinary task, reported on the Team
// board; a run does not start while the last one is still going.
type Schedule struct {
	ID          types.ID `json:"id"`
	ProfileID   types.ID `json:"profileId"`
	Instruction string   `json:"instruction"`
	// EveryMinutes > 0 repeats; else DailyAt ("09:00", local time) does.
	EveryMinutes int       `json:"everyMinutes,omitempty"`
	DailyAt      string    `json:"dailyAt,omitempty"`
	WorkspaceID  types.ID  `json:"workspaceId,omitempty"`
	Enabled      bool      `json:"enabled"`
	LastRun      time.Time `json:"lastRun,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// No timer runs more often than this: a schedule is for routine work.
const minEveryMinutes = 15

func (s Schedule) Describe() string {
	if s.EveryMinutes > 0 {
		if s.EveryMinutes%60 == 0 {
			return fmt.Sprintf("every %d h", s.EveryMinutes/60)
		}
		return fmt.Sprintf("every %d min", s.EveryMinutes)
	}
	return "daily at " + s.DailyAt
}

// due reports whether the schedule should run at now.
func (s Schedule) due(now time.Time) bool {
	if !s.Enabled {
		return false
	}
	if s.EveryMinutes > 0 {
		return s.LastRun.IsZero() || now.Sub(s.LastRun) >= time.Duration(s.EveryMinutes)*time.Minute
	}
	h, m, ok := clock(s.DailyAt)
	if !ok {
		return false
	}
	local := now.Local()
	slot := time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, time.Local)
	return !local.Before(slot) && s.LastRun.Before(slot)
}

func clock(hhmm string) (int, int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(hhmm))
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

func (e *Engine) Schedules(ctx context.Context) ([]Schedule, error) {
	bodies, err := e.st.ListStaffSchedules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Schedule, 0, len(bodies))
	for _, b := range bodies {
		var s Schedule
		if json.Unmarshal([]byte(b), &s) == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

// SaveSchedule adds or changes a schedule. A new one is on, and waits for
// its first slot rather than running at once.
func (e *Engine) SaveSchedule(ctx context.Context, s Schedule) (Schedule, error) {
	if _, err := e.st.GetAgentProfile(ctx, s.ProfileID); err != nil {
		return s, errors.New("no such agent")
	}
	s.Instruction = strings.TrimSpace(s.Instruction)
	if s.Instruction == "" {
		return s, errors.New("a schedule needs an instruction")
	}
	if r := []rune(s.Instruction); len(r) > instructionRunes {
		s.Instruction = string(r[:instructionRunes])
	}
	if s.EveryMinutes > 0 {
		if s.EveryMinutes < minEveryMinutes {
			s.EveryMinutes = minEveryMinutes
		}
		s.DailyAt = ""
	} else {
		if _, _, ok := clock(s.DailyAt); !ok {
			return s, errors.New(`give "every" minutes or a daily time like "09:00"`)
		}
		s.EveryMinutes = 0
	}
	if s.ID == "" {
		s.ID, s.CreatedAt, s.Enabled = id.NewID(), time.Now().UTC(), true
		// a repeating one counts from now; a daily one from its next slot
		s.LastRun = time.Now().UTC()
	}
	return s, e.putSchedule(ctx, s)
}

func (e *Engine) putSchedule(ctx context.Context, s Schedule) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return e.st.PutStaffSchedule(ctx, s.ID, s.ProfileID, string(b), s.CreatedAt)
}

func (e *Engine) DeleteSchedule(ctx context.Context, scheduleID types.ID) error {
	return e.st.DeleteStaffSchedule(ctx, scheduleID)
}

// Tick runs the schedules that are due. The engine's loop calls it; tests
// call it with a clock of their own.
func (e *Engine) Tick(ctx context.Context, now time.Time) int {
	list, err := e.Schedules(ctx)
	if err != nil {
		return 0
	}
	started := 0
	for _, s := range list {
		if !s.due(now) || e.scheduleBusy(ctx, s) {
			continue
		}
		s.LastRun = now.UTC()
		if err := e.putSchedule(ctx, s); err != nil {
			continue
		}
		brief := s.Instruction + "\n\nThis is a scheduled task (" + s.Describe() + "). Report what you found and did."
		if _, err := e.Assign(ctx, AssignOpts{ProfileID: s.ProfileID, Brief: brief, WorkspaceID: s.WorkspaceID, ScheduleID: s.ID, Origin: s.Describe()}); err == nil {
			started++
		}
	}
	return started + e.checkMonitors(ctx, now)
}

func (e *Engine) scheduleBusy(ctx context.Context, s Schedule) bool {
	tasks, err := e.Tasks(ctx, s.ProfileID, 20)
	if err != nil {
		return true
	}
	for _, t := range tasks {
		if t.ScheduleID == s.ID && t.Status == StatusRunning {
			return true
		}
	}
	return false
}

// Start runs the schedules every half minute until the engine closes.
func (e *Engine) Start() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-e.base.Done():
				return
			case now := <-t.C:
				e.Tick(e.base, now)
			}
		}
	}()
}
