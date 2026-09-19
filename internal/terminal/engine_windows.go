//go:build windows

package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
	"golang.org/x/sys/windows"
)

var ErrNotFound = errors.New("terminal: not found")

type Engine struct {
	store *store.Store
	bus   *eventbus.Bus
	mu    sync.Mutex
	sess  map[types.ID]*live
}

type live struct {
	meta   types.TerminalSession
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cancel context.CancelFunc
	mu     sync.Mutex
	buf    []byte
}

func New(s *store.Store, bus *eventbus.Bus) *Engine {
	return &Engine{store: s, bus: bus, sess: map[types.ID]*live{}}
}

func (e *Engine) Spawn(ctx context.Context, opts SpawnOpts) (types.TerminalSession, error) {
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	if opts.Kind == "" {
		opts.Kind = types.TermUser
	}
	shell := opts.Shell
	var args []string
	if len(opts.Command) > 0 {
		shell = opts.Command[0]
		args = opts.Command[1:]
	} else if opts.SSH != nil {
		shell, args = sshCommand(*opts.SSH)
	} else if shell == "" {
		shell, args = defaultShell()
	}
	cmd := exec.CommandContext(ctx, shell, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return types.TerminalSession{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return types.TerminalSession{}, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return types.TerminalSession{}, err
	}
	now := time.Now().UTC()
	meta := types.TerminalSession{
		ID: id.NewID(), Kind: opts.Kind, OwnerID: opts.OwnerID, WorkspaceID: opts.WorkspaceID,
		Title: opts.Title, Cwd: opts.Cwd, Shell: shell, Cols: opts.Cols, Rows: opts.Rows,
		PID: cmd.Process.Pid, Status: types.TermRunning, SSH: opts.SSH, Persistent: opts.Persistent,
		CreatedAt: now, UpdatedAt: now,
	}
	if meta.Title == "" {
		meta.Title = shell
	}
	lctx, cancel := context.WithCancel(context.Background())
	lv := &live{meta: meta, cmd: cmd, stdin: stdin, stdout: stdout, cancel: cancel}
	e.mu.Lock()
	e.sess[meta.ID] = lv
	e.mu.Unlock()
	if e.store != nil {
		_ = e.store.UpsertTerminal(ctx, meta)
	}
	go e.pump(lctx, lv)
	go e.wait(lv)
	return meta, nil
}

func (e *Engine) pump(ctx context.Context, lv *live) {
	buf := make([]byte, 4096)
	for {
		n, err := lv.stdout.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			lv.mu.Lock()
			lv.buf = append(lv.buf, chunk...)
			if len(lv.buf) > 256*1024 {
				lv.buf = lv.buf[len(lv.buf)-256*1024:]
			}
			lv.mu.Unlock()
			if e.bus != nil {
				e.bus.Publish(types.Event{
					Type: types.EventTerminalData, Topic: "terminal." + string(lv.meta.ID),
					Payload: map[string]any{"id": string(lv.meta.ID), "data": string(chunk)},
				})
			}
		}
		if err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (e *Engine) wait(lv *live) {
	err := lv.cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	lv.mu.Lock()
	lv.meta.Status = types.TermExited
	lv.meta.UpdatedAt = time.Now().UTC()
	meta := lv.meta
	lv.mu.Unlock()
	if e.store != nil {
		_ = e.store.UpsertTerminal(context.Background(), meta)
	}
	if e.bus != nil {
		e.bus.Publish(types.Event{Type: types.EventTerminalExit, Topic: "terminal." + string(meta.ID), Payload: map[string]any{"id": string(meta.ID), "exitCode": code}})
	}
}

func (e *Engine) Write(id types.ID, data []byte) error {
	lv, err := e.get(id)
	if err != nil {
		return err
	}
	_, err = lv.stdin.Write(data)
	return err
}

func (e *Engine) Resize(id types.ID, cols, rows int) error {
	lv, err := e.get(id)
	if err != nil {
		return err
	}
	lv.mu.Lock()
	lv.meta.Cols = cols
	lv.meta.Rows = rows
	lv.mu.Unlock()
	return nil
}

func (e *Engine) Kill(id types.ID) error {
	lv, err := e.get(id)
	if err != nil {
		return err
	}
	lv.cancel()
	if lv.cmd.Process != nil {
		_ = lv.cmd.Process.Kill()
	}
	return nil
}

func (e *Engine) Restart(ctx context.Context, id types.ID) (types.TerminalSession, error) {
	lv, err := e.get(id)
	if err != nil {
		return types.TerminalSession{}, err
	}
	lv.mu.Lock()
	opts := SpawnOpts{Kind: lv.meta.Kind, OwnerID: lv.meta.OwnerID, WorkspaceID: lv.meta.WorkspaceID, Cwd: lv.meta.Cwd, Shell: lv.meta.Shell, Cols: lv.meta.Cols, Rows: lv.meta.Rows, Title: lv.meta.Title, Persistent: lv.meta.Persistent, SSH: lv.meta.SSH}
	lv.mu.Unlock()
	_ = e.Kill(id)
	return e.Spawn(ctx, opts)
}

func (e *Engine) Detach(id types.ID) error {
	lv, err := e.get(id)
	if err != nil {
		return err
	}
	lv.mu.Lock()
	lv.meta.Status = types.TermDetached
	lv.mu.Unlock()
	if e.store != nil {
		return e.store.UpsertTerminal(context.Background(), lv.meta)
	}
	return nil
}

func (e *Engine) Attach(id types.ID) (types.TerminalSession, []byte, error) {
	lv, err := e.get(id)
	if err != nil {
		return types.TerminalSession{}, nil, err
	}
	lv.mu.Lock()
	defer lv.mu.Unlock()
	if lv.meta.Status == types.TermDetached {
		lv.meta.Status = types.TermRunning
	}
	return lv.meta, append([]byte(nil), lv.buf...), nil
}

func (e *Engine) Get(id types.ID) (types.TerminalSession, error) {
	lv, err := e.get(id)
	if err != nil {
		if e.store != nil {
			return e.store.GetTerminal(context.Background(), id)
		}
		return types.TerminalSession{}, err
	}
	lv.mu.Lock()
	defer lv.mu.Unlock()
	return lv.meta, nil
}

func (e *Engine) List() []types.TerminalSession {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]types.TerminalSession, 0, len(e.sess))
	for _, lv := range e.sess {
		lv.mu.Lock()
		out = append(out, lv.meta)
		lv.mu.Unlock()
	}
	return out
}

func (e *Engine) get(id types.ID) (*live, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	lv, ok := e.sess[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return lv, nil
}

func defaultShell() (string, []string) { return "powershell.exe", nil }

func sshCommand(t types.SSHTarget) (string, []string) {
	port := t.Port
	if port == 0 {
		port = 22
	}
	args := []string{"-tt", "-p", fmt.Sprintf("%d", port)}
	if t.KeyPath != "" {
		key := t.KeyPath
		if strings.HasPrefix(key, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				key = filepath.Join(home, key[2:])
			}
		}
		args = append(args, "-i", key, "-o", "IdentitiesOnly=yes")
	}
	if t.AuthMethod == "agent" || t.AuthMethod == "key" {
		args = append(args, "-o", "BatchMode=yes")
	}
	target := t.Host
	if t.User != "" {
		target = t.User + "@" + t.Host
	}
	args = append(args, target)
	return "ssh", args
}

func CopyWriter(w io.Writer) io.Writer { return w }
