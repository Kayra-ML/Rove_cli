package main

import (
	"os/exec"
	"runtime"
)

// Notify shows a system notification.
//   - macOS: Notification Center via osascript
//   - Linux: notify-send (if available)
//   - Windows: PowerShell Windows.UI.Notifications toast (Win 10/11)
func (a *App) Notify(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			title, body).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			return exec.Command("notify-send", "--app-name=Rove", title, body).Run()
		}
	case "windows":
		// PowerShell one-liner using Windows.UI.Notifications (Win 10+).
		// Falls back silently on older Windows versions.
		script := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$template.GetElementsByTagName("text")[0].AppendChild($template.CreateTextNode("` + title + `")) > $null
$template.GetElementsByTagName("text")[1].AppendChild($template.CreateTextNode("` + body + `")) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Rove Code").Show($toast)`
		return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run()
	}
	return nil
}
