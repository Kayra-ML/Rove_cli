//go:build !darwin

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// UpdateInfo is what the app shows about a newer version.
// On non-Darwin platforms in-place app update is not supported;
// the user is directed to the installer instead.
type UpdateInfo struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	CanInstall bool   `json:"canInstall"`
	NotesURL   string `json:"notesUrl"`
	Error      string `json:"error,omitempty"`
}

// CheckUpdate is not implemented for this platform; it always
// returns an error directing the user to the manual installer.
func (a *App) CheckUpdate() UpdateInfo {
	return UpdateInfo{
		Error: fmt.Sprintf("automatic updates are not supported on %s; use the installer", runtime.GOOS),
	}
}

// InstallUpdate is not implemented for this platform.
func (a *App) InstallUpdate() error {
	return errors.New("automatic in-place update is not supported on this platform")
}

// UpdateInTerminal opens the installer script in the platform's
// default terminal so the user can update manually.
//
//   - Windows: PowerShell in a new window running install-windows.ps1
//   - Linux:   xterm (or x-terminal-emulator) running install.sh
func (a *App) UpdateInTerminal() error {
	const repo = "Kayra-ML/Rove_cli"
	switch runtime.GOOS {
	case "windows":
		script := `iwr https://raw.githubusercontent.com/` + repo + `/main/install-windows.ps1 | iex`
		return exec.Command("powershell", "-NoExit", "-Command", script).Start()
	default:
		script := `curl -fsSL https://raw.githubusercontent.com/` + repo + `/main/install.sh | sh ; read -p "Press Enter to close"`
		for _, term := range []string{"x-terminal-emulator", "xterm", "gnome-terminal", "konsole"} {
			if _, err := exec.LookPath(term); err == nil {
				return exec.Command(term, "-e", "sh", "-c", script).Start()
			}
		}
		return errors.New("no known terminal emulator found; run the install script manually")
	}
}
