//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockDataDir takes an exclusive lock on the data dir for this process's
// lifetime. Two daemons on one data dir share a database but not their
// in-memory state (runs, event bus), and the second one steals the IPC
// socket — so requests and live events end up split between them.
// The lock file names the holder: its pid, then the address it serves on.
func lockDataDir(dir, addr string) (release func(), err error) {
	path := filepath.Join(dir, "rovecode.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			pid, _ := lockHolder(dir)
			return nil, fmt.Errorf("%w (pid %s)", ErrAlreadyRunning, pid)
		}
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("%d\n%s\n", os.Getpid(), addr)), 0)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
