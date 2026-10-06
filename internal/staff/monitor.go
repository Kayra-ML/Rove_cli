package staff

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

// Monitor watches something outside the project — failing CI runs, new
// bug reports, new errors in production — by running a command on a timer
// (gh, sentry-cli, curl…), and gives an agent a task when the command's
// output has lines it has not seen before. A check that finds nothing new
// costs no tokens: no model is asked. The first check only learns what is
// there already, so setting one up does not bring a flood of old items.
type Monitor struct {
	ID          types.ID `json:"id"`
	ProfileID   types.ID `json:"profileId"`
	Name        string   `json:"name"`
	Command     string   `json:"command"`
	Instruction string   `json:"instruction"`
	// EveryMinutes between checks; at least minMonitorMinutes.
	EveryMinutes int       `json:"everyMinutes"`
	WorkspaceID  types.ID  `json:"workspaceId,omitempty"`
	Enabled      bool      `json:"enabled"`
	LastRun      time.Time `json:"lastRun,omitempty"`
	LastError    string    `json:"lastError,omitempty"`
	// Seen are hashes of the lines already reported, newest last.
	Seen      []string  `json:"seen,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

const (
	minMonitorMinutes = 5
	monitorTimeout    = 60 * time.Second
	monitorLines      = 50  // lines read from one check
	monitorNewMax     = 20  // new lines handed to the agent at most
	monitorSeenMax    = 500 // hashes remembered
	commandRunes      = 2000
)

func (e *Engine) Monitors(ctx context.Context) ([]Monitor, error) {
	bodies, err := e.st.ListStaffMonitors(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Monitor, 0, len(bodies))
	for _, b := range bodies {
		var m Monitor
		if json.Unmarshal([]byte(b), &m) == nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// SaveMonitor adds or changes a monitor. A new one checks at the next tick
// to learn what is there already.
func (e *Engine) SaveMonitor(ctx context.Context, m Monitor) (Monitor, error) {
	if _, err := e.st.GetAgentProfile(ctx, m.ProfileID); err != nil {
		return m, errors.New("no such agent")
	}
	m.Command = strings.TrimSpace(m.Command)
	if m.Command == "" {
		return m, errors.New("a monitor needs a command")
	}
	if r := []rune(m.Command); len(r) > commandRunes {
		return m, errors.New("the command is too long")
	}
	m.Instruction = strings.TrimSpace(m.Instruction)
	if m.Instruction == "" {
		m.Instruction = "Look into these new items and fix what you can; report what you found."
	}
	if r := []rune(m.Instruction); len(r) > instructionRunes {
		m.Instruction = string(r[:instructionRunes])
	}
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		m.Name = clip(m.Command, 40)
	}
	if m.EveryMinutes < minMonitorMinutes {
		m.EveryMinutes = minMonitorMinutes
	}
	if m.ID == "" {
		m.ID, m.CreatedAt, m.Enabled = id.NewID(), time.Now().UTC(), true
	} else if old, ok := e.monitor(ctx, m.ID); ok {
		// what it has seen is the engine's to keep, whatever the edit says
		m.Seen, m.LastRun = old.Seen, old.LastRun
		if old.Command != m.Command {
			m.Seen, m.LastRun = nil, time.Time{} // a new command learns afresh
		}
	}
	return m, e.putMonitor(ctx, m)
}

func (e *Engine) monitor(ctx context.Context, mid types.ID) (Monitor, bool) {
	list, err := e.Monitors(ctx)
	if err != nil {
		return Monitor{}, false
	}
	for _, m := range list {
		if m.ID == mid {
			return m, true
		}
	}
	return Monitor{}, false
}

func (e *Engine) putMonitor(ctx context.Context, m Monitor) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return e.st.PutStaffMonitor(ctx, m.ID, m.ProfileID, string(b), m.CreatedAt)
}

func (e *Engine) DeleteMonitor(ctx context.Context, mid types.ID) error {
	return e.st.DeleteStaffMonitor(ctx, mid)
}

// checkMonitors runs the monitors that are due.
func (e *Engine) checkMonitors(ctx context.Context, now time.Time) int {
	list, err := e.Monitors(ctx)
	if err != nil {
		return 0
	}
	started := 0
	for _, m := range list {
		if !m.Enabled || (!m.LastRun.IsZero() && now.Sub(m.LastRun) < time.Duration(m.EveryMinutes)*time.Minute) {
			continue
		}
		if e.monitorBusy(ctx, m) {
			continue
		}
		if e.check(ctx, &m, now) {
			started++
		}
	}
	return started
}

func (e *Engine) monitorBusy(ctx context.Context, m Monitor) bool {
	tasks, err := e.Tasks(ctx, m.ProfileID, 20)
	if err != nil {
		return true
	}
	for _, t := range tasks {
		if t.MonitorID == m.ID && t.Status == StatusRunning {
			return true
		}
	}
	return false
}

// check runs one monitor once; true when it handed the agent a task.
func (e *Engine) check(ctx context.Context, m *Monitor, now time.Time) bool {
	first := m.LastRun.IsZero() && len(m.Seen) == 0
	m.LastRun = now.UTC()
	dir := ""
	if e.WorkspacePath != nil && m.WorkspaceID != "" {
		dir = e.WorkspacePath(ctx, m.WorkspaceID)
	}
	out, err := e.runCommand(ctx, m.Command, dir)
	if err != nil {
		m.LastError = clip(err.Error(), 300)
		_ = e.putMonitor(ctx, *m)
		return false
	}
	m.LastError = ""
	seen := map[string]bool{}
	for _, h := range m.Seen {
		seen[h] = true
	}
	var fresh []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		h := lineHash(l)
		if seen[h] {
			continue
		}
		seen[h] = true
		m.Seen = append(m.Seen, h)
		fresh = append(fresh, l)
		if len(fresh) >= monitorLines {
			break
		}
	}
	if len(m.Seen) > monitorSeenMax {
		m.Seen = m.Seen[len(m.Seen)-monitorSeenMax:]
	}
	_ = e.putMonitor(ctx, *m)
	if first || len(fresh) == 0 {
		return false
	}
	if len(fresh) > monitorNewMax {
		fresh = fresh[:monitorNewMax]
	}
	var b strings.Builder
	b.WriteString(m.Instruction)
	b.WriteString("\n\nThe monitor \"" + m.Name + "\" (`" + m.Command + "`) found these new items:\n")
	for _, l := range fresh {
		b.WriteString("- " + clip(l, 300) + "\n")
	}
	b.WriteString("\nLook at the real thing (logs, the run, the issue) before you change code.")
	_, err = e.Assign(ctx, AssignOpts{ProfileID: m.ProfileID, Brief: b.String(), WorkspaceID: m.WorkspaceID, MonitorID: m.ID, Origin: m.Name})
	return err == nil
}

// runCommand runs a monitor's command in the project folder, with the
// user's shell and a time limit.
func (e *Engine) runCommand(ctx context.Context, command, dir string) (string, error) {
	if e.RunCommand != nil {
		return e.RunCommand(ctx, command, dir)
	}
	ctx, cancel := context.WithTimeout(ctx, monitorTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

func lineHash(l string) string {
	s := sha1.Sum([]byte(l))
	return hex.EncodeToString(s[:8])
}

// MonitorPreset is a ready-made monitor the app offers.
type MonitorPreset struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Instruction string `json:"instruction"`
	Every       int    `json:"every"`
	Needs       string `json:"needs"`
}

// MonitorPresets are the monitors offered ready to use. Each needs a tool
// of the user's own, signed in: gh for GitHub, sentry-cli for Sentry.
var MonitorPresets = []MonitorPreset{
	{
		Key: "gh-ci", Name: "GitHub CI failures", Every: 15, Needs: "gh",
		Command:     `gh run list --status failure --limit 10 --json databaseId,displayTitle,headBranch,url --jq '.[] | "\(.databaseId) \(.displayTitle) [\(.headBranch)] \(.url)"'`,
		Instruction: "A CI run failed. Read its log (gh run view <id> --log-failed), find the cause and fix it.",
	},
	{
		Key: "gh-bugs", Name: "New GitHub bug issues", Every: 30, Needs: "gh",
		Command:     `gh issue list --label bug --state open --limit 20 --json number,title,url --jq '.[] | "#\(.number) \(.title) \(.url)"'`,
		Instruction: "A new bug was reported. Read the issue (gh issue view <number>), reproduce it if you can, and fix it.",
	},
	{
		Key: "sentry", Name: "New Sentry errors", Every: 15, Needs: "sentry-cli",
		Command:     `sentry-cli issues list --status unresolved --max-rows 20`,
		Instruction: "Production has a new error. Find its root cause in the code and fix it.",
	},
}
