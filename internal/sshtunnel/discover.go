package sshtunnel

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Discovered is an SSH server found on this machine, ready to add.
type Discovered struct {
	// Alias is the Host name in ~/.ssh/config; connecting by it lets ssh
	// apply everything the config says (HostName, User, Port, ProxyJump…).
	Alias    string `json:"alias,omitempty"`
	HostName string `json:"hostName"`
	User     string `json:"user,omitempty"`
	Port     int    `json:"port,omitempty"`
	KeyPath  string `json:"keyPath,omitempty"`
	Source   string `json:"source"` // "config" | "known_hosts"
}

// gitHosts are code forges, not servers to open a shell on.
var gitHosts = map[string]bool{
	"github.com": true, "gitlab.com": true, "bitbucket.org": true, "ssh.github.com": true,
	"ssh.dev.azure.com": true, "vs-ssh.visualstudio.com": true, "codeberg.org": true,
	"git.sr.ht": true, "source.developers.google.com": true,
}

const maxDiscovered = 100

// Discover lists the SSH servers this machine already knows: the Host
// entries of ~/.ssh/config (and the files it Includes), then hosts from
// ~/.ssh/known_hosts that the config does not name. Wildcard patterns,
// hashed known_hosts entries, local addresses and code forges are left out.
func Discover(home string) []Discovered {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	sshDir := filepath.Join(home, ".ssh")
	var out []Discovered
	seen := map[string]bool{}
	add := func(d Discovered) {
		key := strings.ToLower(d.HostName)
		if d.Alias != "" {
			key = "alias:" + strings.ToLower(d.Alias)
		}
		if seen[key] || len(out) >= maxDiscovered {
			return
		}
		seen[key] = true
		if d.Alias != "" {
			seen[strings.ToLower(d.HostName)] = true
		}
		out = append(out, d)
	}

	for _, d := range parseConfig(filepath.Join(sshDir, "config"), sshDir, home, 0) {
		if !skipHost(d.HostName) {
			add(d)
		}
	}
	for _, d := range parseKnownHosts(filepath.Join(sshDir, "known_hosts")) {
		if !skipHost(d.HostName) && !seen[strings.ToLower(d.HostName)] {
			add(d)
		}
	}
	return out
}

func skipHost(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	return h == "" || gitHosts[h] || h == "localhost" || strings.HasPrefix(h, "127.") || h == "::1"
}

func wildcard(p string) bool { return strings.ContainsAny(p, "*?!") }

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// parseConfig reads an ssh_config file. Settings under "Host *" fill in
// what an entry does not set itself; Match blocks are skipped.
func parseConfig(path, sshDir, home string, depth int) []Discovered {
	f, err := os.Open(path)
	if err != nil || depth > 4 {
		return nil
	}
	defer f.Close()

	type block struct {
		names  []string
		values map[string]string
	}
	var blocks []*block
	var defaults = map[string]string{}
	var cur *block
	inDefaults, inMatch := false, false
	var included []Discovered

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val := splitDirective(line)
		switch key {
		case "include":
			for _, pat := range strings.Fields(val) {
				pat = expandHome(pat, home)
				if !filepath.IsAbs(pat) {
					pat = filepath.Join(sshDir, pat)
				}
				matches, _ := filepath.Glob(pat)
				for _, m := range matches {
					included = append(included, parseConfig(m, sshDir, home, depth+1)...)
				}
			}
			continue
		case "host":
			inMatch = false
			names := strings.Fields(val)
			allWild := true
			var concrete []string
			for _, n := range names {
				if !wildcard(n) {
					allWild = false
					concrete = append(concrete, n)
				}
			}
			inDefaults = allWild && len(names) == 1 && names[0] == "*"
			if allWild {
				cur = nil
				continue
			}
			cur = &block{names: concrete, values: map[string]string{}}
			blocks = append(blocks, cur)
			continue
		case "match":
			inMatch, inDefaults, cur = true, false, nil
			continue
		}
		if inMatch {
			continue
		}
		target := map[string]string(nil)
		switch {
		case cur != nil:
			target = cur.values
		case inDefaults:
			target = defaults
		}
		if target != nil {
			if _, set := target[key]; !set { // ssh: the first value wins
				target[key] = val
			}
		}
	}

	var out []Discovered
	for _, b := range blocks {
		get := func(k string) string {
			if v, ok := b.values[k]; ok {
				return v
			}
			return defaults[k]
		}
		for _, name := range b.names {
			d := Discovered{Alias: name, HostName: name, Source: "config"}
			if hn := get("hostname"); hn != "" {
				d.HostName = strings.ReplaceAll(hn, "%h", name)
			}
			d.User = get("user")
			if p, err := strconv.Atoi(get("port")); err == nil && p > 0 {
				d.Port = p
			}
			if k := get("identityfile"); k != "" {
				d.KeyPath = strings.Trim(k, `"`)
			}
			out = append(out, d)
		}
	}
	return append(out, included...)
}

// splitDirective reads "Key value" or "Key=value"; the key is lower-cased.
func splitDirective(line string) (string, string) {
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), ""
	}
	key := strings.ToLower(line[:i])
	val := strings.TrimLeft(line[i:], " \t=")
	return key, strings.TrimSpace(val)
}

// parseKnownHosts reads the hosts this machine has connected to.
func parseKnownHosts(path string) []Discovered {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	byHost := map[string]Discovered{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "@") || strings.HasPrefix(line, "|") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, h := range strings.Split(fields[0], ",") {
			d := Discovered{HostName: h, Source: "known_hosts"}
			// [host]:port
			if strings.HasPrefix(h, "[") {
				end := strings.Index(h, "]")
				if end < 0 {
					continue
				}
				d.HostName = h[1:end]
				if p, err := strconv.Atoi(strings.TrimPrefix(h[end+1:], ":")); err == nil {
					d.Port = p
				}
			}
			if wildcard(d.HostName) {
				continue
			}
			if _, ok := byHost[d.HostName]; !ok {
				byHost[d.HostName] = d
			}
		}
	}
	out := make([]Discovered, 0, len(byHost))
	for _, d := range byHost {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HostName < out[j].HostName })
	return out
}
