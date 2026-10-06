package terminal

// What the Unix (pty) and Windows (pipes) engines share; each platform's
// file defines its own live session type and how it spawns and writes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

var ErrNotFound = errors.New("terminal: not found")

type Engine struct {
	store *store.Store
	bus   *eventbus.Bus
	mu    sync.Mutex
	sess  map[types.ID]*live
}

func New(s *store.Store, bus *eventbus.Bus) *Engine {
	return &Engine{store: s, bus: bus, sess: map[types.ID]*live{}}
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

func sshCommand(t types.SSHTarget) (string, []string) {
	// no port given: ssh takes it from ~/.ssh/config (or 22), so a
	// config alias keeps its own Port, User, ProxyJump…
	args := []string{"-tt"}
	if t.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", t.Port))
	}
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
