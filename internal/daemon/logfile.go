package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/mattn/go-isatty"
)

// logLimit is how big the log grows before it is set aside as .1 (one old
// copy is kept): enough for days of use, small enough to read and send.
const logLimit = 5 << 20

// LogPath is where the daemon writes its log.
func LogPath(dataDir string) string {
	return filepath.Join(dataDir, "logs", "rovecode-daemon.log")
}

// OpenLog sends the daemon's log, and a crash's stack, to its log file —
// unless it runs in a terminal, where the person watching sees them. A
// daemon the desktop app starts from the Finder has no terminal, and what it
// printed went nowhere.
func OpenLog(dataDir string) (io.Closer, error) {
	// /dev/null is a character device too; only a real terminal counts
	if isatty.IsTerminal(os.Stderr.Fd()) {
		log.SetFlags(log.LstdFlags)
		return io.NopCloser(nil), nil
	}
	p := LogPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(p); err == nil && fi.Size() > logLimit {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
	os.Stdout, os.Stderr = f, f
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		fmt.Fprintf(f, "crash output: %v\n", err)
	}
	return f, nil
}
