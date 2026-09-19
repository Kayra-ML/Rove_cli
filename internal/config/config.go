package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	DataDir    string `json:"dataDir"`
	ListenHTTP string `json:"listenHttp"`
	ListenIPC  string `json:"listenIpc"`
	LogLevel   string `json:"logLevel"`
}

func envHome() string {
	if x := os.Getenv("ROVECODE_HOME"); x != "" {
		return x
	}
	return os.Getenv("AETHER_HOME")
}

func DefaultDataDir() string {
	if x := envHome(); x != "" {
		return x
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return firstExisting(
			filepath.Join(home, "Library", "Application Support", "Rove Code"),
			filepath.Join(home, "Library", "Application Support", "Aether"),
		)
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return firstExisting(filepath.Join(base, "Rove Code"), filepath.Join(base, "Aether"))
	default:
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".local", "share")
		}
		return firstExisting(filepath.Join(xdg, "rovecode"), filepath.Join(xdg, "aether"))
	}
}

func firstExisting(preferred, legacy string) string {
	if dataDirLooksUsed(preferred) {
		return preferred
	}
	if dataDirLooksUsed(legacy) {
		return legacy
	}
	return preferred
}

func dataDirLooksUsed(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{
		"rovecode.db", "aether.db", "auth.token", "config.json",
		"rovecode.sock", "aether.sock", "rovecode.pid", "aetherd.pid",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func Load(path string) (Config, error) {
	cfg := Config{
		DataDir:    DefaultDataDir(),
		ListenHTTP: "127.0.0.1:7420",
		ListenIPC:  defaultIPCPath(),
		LogLevel:   "info",
	}
	if path == "" {
		path = filepath.Join(cfg.DataDir, "config.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.ListenHTTP == "" {
		cfg.ListenHTTP = "127.0.0.1:7420"
	}
	if cfg.ListenIPC == "" {
		cfg.ListenIPC = defaultIPCPath()
	}
	return cfg, nil
}

func (c Config) EnsureDirs() error {
	for _, d := range []string{
		c.DataDir,
		filepath.Join(c.DataDir, "skills"),
		filepath.Join(c.DataDir, "registry"),
		filepath.Join(c.DataDir, "secrets"),
		filepath.Join(c.DataDir, "logs"),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func defaultIPCPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\rovecode`
	}
	dir := DefaultDataDir()
	for _, name := range []string{"rovecode.sock", "aether.sock"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(dir, "rovecode.sock")
}

func DBPath(dataDir string) string {
	for _, name := range []string{"rovecode.db", "aether.db"} {
		p := filepath.Join(dataDir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(dataDir, "rovecode.db")
}

func TokenPath(dataDir string) string {
	return filepath.Join(dataDir, "auth.token")
}

func TokenSearchPaths(dataDir string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(TokenPath(dataDir))
	home, _ := os.UserHomeDir()
	add(filepath.Join(home, ".local", "share", "rovecode", "auth.token"))
	add(filepath.Join(home, ".local", "share", "aether", "auth.token"))
	add(filepath.Join(home, "Library", "Application Support", "Rove Code", "auth.token"))
	add(filepath.Join(home, "Library", "Application Support", "Aether", "auth.token"))
	if app := os.Getenv("APPDATA"); app != "" {
		add(filepath.Join(app, "Rove Code", "auth.token"))
		add(filepath.Join(app, "Aether", "auth.token"))
	}
	return out
}
