//go:build darwin

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Updates come from the GitHub releases of the repository the app is built
// from. A release that carries the macOS app (updateAsset, listed in
// SHA256SUMS) is installed in place; one without it can still be installed
// with the source installer in a terminal.
const (
	updateRepo  = "Kayra-ML/Rove_cli"
	updateAsset = "RoveCode-macos-universal.zip"
	installCmd  = "curl -fsSL https://raw.githubusercontent.com/" + updateRepo + "/main/install-desktop.sh | bash"
)

// UpdateInfo is what the app shows about a newer version.
type UpdateInfo struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	CanInstall bool   `json:"canInstall"` // the release has the app: install in place
	NotesURL   string `json:"notesUrl"`
	Error      string `json:"error,omitempty"`
}

type ghRelease struct {
	Tag    string `json:"tag_name"`
	URL    string `json:"html_url"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r ghRelease) asset(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

var httpc = &http.Client{Timeout: 5 * time.Minute}

func latestRelease(ctx context.Context) (ghRelease, error) {
	var r ghRelease
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := httpc.Do(req)
	if err != nil {
		return r, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return r, fmt.Errorf("releases: HTTP %d", res.StatusCode)
	}
	return r, json.NewDecoder(res.Body).Decode(&r)
}

// appBundle is the .app this program runs from, or "" when it does not
// (wails dev, another OS).
func appBundle() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	b := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(b, ".app") {
		return ""
	}
	return b
}

func bundleVersion(bundle string) string {
	out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-", filepath.Join(bundle, "Contents", "Info.plist")).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// newer reports whether version a is after b ("v0.2.10" > "0.2.9").
func newer(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// CheckUpdate asks GitHub for the latest release.
func (a *App) CheckUpdate() UpdateInfo {
	bundle := appBundle()
	info := UpdateInfo{Current: bundleVersion(bundle)}
	if bundle == "" || info.Current == "" {
		info.Error = "not running from an installed app"
		return info
	}
	r, err := latestRelease(a.ctx)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Latest = strings.TrimPrefix(r.Tag, "v")
	info.NotesURL = r.URL
	info.Available = newer(r.Tag, info.Current)
	info.CanInstall = r.asset(updateAsset) != "" && r.asset("SHA256SUMS") != ""
	return info
}

// InstallUpdate downloads the latest app, checks it against the release's
// SHA256SUMS and its code signature, puts it in place of this one and
// starts it. The new app replaces an older daemon itself when it starts.
func (a *App) InstallUpdate() error {
	bundle := appBundle()
	if bundle == "" {
		return errors.New("not running from an installed app")
	}
	r, err := latestRelease(a.ctx)
	if err != nil {
		return err
	}
	zipURL, sumsURL := r.asset(updateAsset), r.asset("SHA256SUMS")
	if zipURL == "" || sumsURL == "" {
		return errors.New("this release has no app to install; update in the terminal")
	}
	tmp, err := os.MkdirTemp("", "rovecode-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	zipPath := filepath.Join(tmp, updateAsset)
	if err := download(a.ctx, zipURL, zipPath); err != nil {
		return err
	}
	sumsPath := filepath.Join(tmp, "SHA256SUMS")
	if err := download(a.ctx, sumsURL, sumsPath); err != nil {
		return err
	}
	if err := checkSum(zipPath, sumsPath, updateAsset); err != nil {
		return err
	}
	unpacked := filepath.Join(tmp, "app")
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", zipPath, unpacked).CombinedOutput(); err != nil {
		return fmt.Errorf("unpack: %v %s", err, out)
	}
	apps, _ := filepath.Glob(filepath.Join(unpacked, "*.app"))
	if len(apps) != 1 {
		return errors.New("the download holds no app")
	}
	if err := sameSigner(bundle, apps[0]); err != nil {
		return err
	}
	if err := swapBundle(bundle, apps[0]); err != nil {
		return err
	}
	// start the new app once this one has gone
	relaunch := exec.Command("/bin/sh", "-c", `sleep 1; /usr/bin/open -n "$0"`, bundle)
	if err := relaunch.Start(); err != nil {
		return err
	}
	_ = relaunch.Process.Release()
	wruntime.Quit(a.ctx)
	return nil
}

// UpdateInTerminal runs the source installer in Terminal, for a release
// that does not carry the app.
func (a *App) UpdateInTerminal() error {
	script := fmt.Sprintf("tell application \"Terminal\"\nactivate\ndo script %q\nend tell", installCmd)
	return exec.Command("/usr/bin/osascript", "-e", script).Start()
}

func download(ctx context.Context, url, dest string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("download %s: HTTP %d", filepath.Base(dest), res.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// checkSum compares a file with its line in a SHA256SUMS file.
func checkSum(path, sumsPath, name string) error {
	sf, err := os.Open(sumsPath)
	if err != nil {
		return err
	}
	defer sf.Close()
	want := ""
	sc := bufio.NewScanner(sf)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return fmt.Errorf("%s is not in SHA256SUMS", name)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

// sameSigner checks the new app's signature, and that a Developer ID signed
// app is only replaced by one from the same team.
func sameSigner(current, next string) error {
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", next).CombinedOutput(); err != nil {
		return fmt.Errorf("the new app's signature does not hold: %s", strings.TrimSpace(string(out)))
	}
	cur, nxt := teamID(current), teamID(next)
	if cur != "" && cur != nxt {
		return fmt.Errorf("the new app is signed by another team (%q, not %q)", nxt, cur)
	}
	return nil
}

func teamID(app string) string {
	out, _ := exec.Command("/usr/bin/codesign", "-dv", app).CombinedOutput()
	for _, l := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(l, "TeamIdentifier="); ok && v != "not set" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// swapBundle puts next where current is: it is copied next to current
// first (same disk, so the switch is two renames), and current is only
// removed once next is in place; on a failed switch current goes back.
func swapBundle(current, next string) error {
	dir := filepath.Dir(current)
	staged := filepath.Join(dir, ".Rove Code.update.app")
	old := filepath.Join(dir, ".Rove Code.old.app")
	_ = os.RemoveAll(staged)
	_ = os.RemoveAll(old)
	if out, err := exec.Command("/usr/bin/ditto", next, staged).CombinedOutput(); err != nil {
		_ = os.RemoveAll(staged)
		return fmt.Errorf("no permission to update %s: %s", dir, strings.TrimSpace(string(out)))
	}
	if err := os.Rename(current, old); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	if err := os.Rename(staged, current); err != nil {
		_ = os.Rename(old, current)
		_ = os.RemoveAll(staged)
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}
