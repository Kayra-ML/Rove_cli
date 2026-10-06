package main

import (
	"testing"
	"time"

	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestStaleReason(t *testing.T) {
	old := time.Unix(1000, 0)
	cur := daemonHealth{APILevel: protocol.APILevel, Exe: "/usr/local/bin/rovecode", ExeModTime: old.Unix()}
	if staleReason(cur, "/app/rovecode", old) != "" {
		t.Fatal("same age replaced")
	}
	if staleReason(cur, "/app/rovecode", old.Add(time.Hour)) == "" {
		t.Fatal("newer program not used")
	}
	low := cur
	low.APILevel--
	if staleReason(low, "/app/rovecode", old) == "" {
		t.Fatal("older api kept")
	}
	pre := daemonHealth{APILevel: protocol.APILevel}
	if staleReason(pre, "/app/rovecode", old) == "" {
		t.Fatal("daemon without exe info kept")
	}
	busy := low
	busy.ActiveAgents = 1
	if staleReason(busy, "/app/rovecode", old.Add(time.Hour)) != "" {
		t.Fatal("busy daemon stopped")
	}
}
