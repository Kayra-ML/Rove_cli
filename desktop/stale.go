package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/Kayra-ML/rove/internal/daemon"
	"github.com/Kayra-ML/rove/pkg/client"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

type daemonHealth struct {
	APILevel     int    `json:"apiLevel"`
	Exe          string `json:"exe"`
	ExeModTime   int64  `json:"exeModTime"`
	ActiveAgents int    `json:"activeAgents"`
}

// staleReason says why the daemon already running should make way for the
// one this app would start: it speaks an older API, or the program this app
// carries (or the one installed) was written after the running one. An
// update used to need the person to find and kill the old daemon by hand;
// until then the app talked to old code. A daemon in the middle of agent
// work is left alone.
func staleReason(h daemonHealth, cand string, candMod time.Time) string {
	if h.ActiveAgents > 0 {
		return ""
	}
	if h.APILevel < protocol.APILevel {
		return "older api"
	}
	if cand == "" {
		return ""
	}
	// a daemon that does not say what it runs from predates this check
	if h.ExeModTime == 0 {
		return "older daemon"
	}
	if candMod.Unix() > h.ExeModTime {
		return "newer program"
	}
	return ""
}

// replaceStaleDaemon stops a running daemon that staleReason finds older,
// so ensureDaemon starts the current one.
func replaceStaleDaemon() {
	cl, err := client.FromEnv()
	if err != nil {
		return
	}
	raw, err := cl.Call(protocol.MethodPing, nil)
	if err != nil {
		return
	}
	var h daemonHealth
	if json.Unmarshal(raw, &h) != nil {
		return
	}
	cmd := daemon.Command()
	if cmd == nil {
		return
	}
	fi, err := os.Stat(cmd.Path)
	if err != nil {
		return
	}
	why := staleReason(h, cmd.Path, fi.ModTime())
	if why == "" {
		return
	}
	log.Printf("restarting daemon (%s): running %s, starting %s", why, h.Exe, cmd.Path)
	if _, err := cl.Call(protocol.MethodShutdown, nil); err != nil {
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && pingDaemon() {
		time.Sleep(100 * time.Millisecond)
	}
}
