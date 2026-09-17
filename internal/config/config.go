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

func DefaultDataDir() string {
	if x := os.Getenv("AETHER_HOME"); x != "" {
		return x
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Aether")
	case "windows":
		if a := os.Getenv("APPDATA"); a != "" {
			return filepath.Join(a, "Aether")
		}
		return filepath.Join(home, "AppData", "Roaming", "Aether")
	default:
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "aether")
		}
		return filepath.Join(home, ".local", "share", "aether")
	}
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
		return `\\.\pipe\aether`
	}
	return filepath.Join(DefaultDataDir(), "aether.sock")
}

func DBPath(dataDir string) string {
	return filepath.Join(dataDir, "aether.db")
}

func TokenPath(dataDir string) string {
	return filepath.Join(dataDir, "auth.token")
}
