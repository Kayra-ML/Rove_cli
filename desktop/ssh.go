package main

import "github.com/Kayra-ML/rove/internal/sshtunnel"

// DiscoverSSH lists the SSH servers this computer already knows
// (~/.ssh/config and known_hosts). It runs here, on the user's machine,
// even when the daemon runs elsewhere.
func (a *App) DiscoverSSH() []sshtunnel.Discovered {
	return sshtunnel.Discover("")
}
