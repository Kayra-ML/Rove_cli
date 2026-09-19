package sshtunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
)

// SavedHost is a persisted SSH host entry.
type SavedHost struct {
	Alias    string    `json:"alias"`
	Spec     string    `json:"spec"`
	LastUsed time.Time `json:"lastUsed"`
	Note     string    `json:"note,omitempty"`
}

// HostStore manages the list of saved SSH hosts.
type HostStore struct {
	Hosts []SavedHost `json:"hosts"`
	path  string
}

func dataDir() string {
	return config.DefaultDataDir()
}

// LoadHosts loads the host list from disk (creates empty if missing).
func LoadHosts() (*HostStore, error) {
	dir := dataDir()
	path := filepath.Join(dir, "ssh_hosts.json")
	s := &HostStore{path: path}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ssh hosts: read: %w", err)
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("ssh hosts: parse: %w", err)
	}
	return s, nil
}

// Save persists the host list to disk.
func (s *HostStore) Save() error {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

// Add inserts or replaces a host entry.
func (s *HostStore) Add(alias, spec, note string) error {
	if alias == "" {
		return fmt.Errorf("alias required")
	}
	if spec == "" {
		return fmt.Errorf("spec required")
	}
	// Validate spec by parsing
	if _, err := ParseHost(spec); err != nil {
		return fmt.Errorf("invalid host spec: %w", err)
	}
	for i, h := range s.Hosts {
		if h.Alias == alias {
			s.Hosts[i] = SavedHost{Alias: alias, Spec: spec, Note: note, LastUsed: h.LastUsed}
			return s.Save()
		}
	}
	s.Hosts = append(s.Hosts, SavedHost{Alias: alias, Spec: spec, Note: note})
	return s.Save()
}

// Remove deletes a host by alias.
func (s *HostStore) Remove(alias string) error {
	for i, h := range s.Hosts {
		if h.Alias == alias {
			s.Hosts = append(s.Hosts[:i], s.Hosts[i+1:]...)
			return s.Save()
		}
	}
	return fmt.Errorf("host %q not found", alias)
}

// Get looks up a host by alias or by exact spec.
func (s *HostStore) Get(aliasOrSpec string) (*SavedHost, bool) {
	for i := range s.Hosts {
		if s.Hosts[i].Alias == aliasOrSpec || s.Hosts[i].Spec == aliasOrSpec {
			return &s.Hosts[i], true
		}
	}
	return nil, false
}

// UpdateLastUsed sets the LastUsed timestamp for a host.
func (s *HostStore) UpdateLastUsed(alias string) {
	for i := range s.Hosts {
		if s.Hosts[i].Alias == alias {
			s.Hosts[i].LastUsed = time.Now()
			_ = s.Save()
			return
		}
	}
}
