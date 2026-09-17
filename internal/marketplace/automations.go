package marketplace

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/aether-dev/aether/internal/types"
)

//go:embed all:bundled/automations
var bundledAutomations embed.FS

// AutomationManifest is the raw YAML shape of automation.yaml.
type AutomationManifest struct {
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Author       string   `yaml:"author"`
	Description  string   `yaml:"description"`
	Kind         string   `yaml:"kind"`
	Tags         []string `yaml:"tags"`
	EverySeconds int      `yaml:"trigger"`
	Permissions  struct {
		Shell   bool `yaml:"shell"`
		Network bool `yaml:"network"`
		Git     bool `yaml:"git"`
	} `yaml:"permissions"`
	Trigger struct {
		EverySeconds int    `yaml:"every_seconds"`
		Cron         string `yaml:"cron"`
	} `yaml:"trigger"`
}

// ListAutomationTemplates reads all bundled automation.yaml files and returns
// them as AutomationTemplate slice.  installed is a set of already-installed
// automation names so Installed can be marked true.
func ListAutomationTemplates(installed map[string]bool) ([]types.AutomationTemplate, error) {
	var out []types.AutomationTemplate

	err := fs.WalkDir(bundledAutomations, "bundled/automations", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || filepath.Base(path) != "automation.yaml" {
			return nil
		}
		data, rerr := bundledAutomations.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var m AutomationManifest
		if yerr := yaml.Unmarshal(data, &m); yerr != nil {
			return nil
		}
		// resolve every_seconds — YAML trigger block
		every := m.Trigger.EverySeconds
		if every == 0 {
			every = 60
		}
		t := types.AutomationTemplate{
			Name:         m.Name,
			Version:      m.Version,
			Author:       m.Author,
			Description:  m.Description,
			Kind:         m.Kind,
			Tags:         m.Tags,
			EverySeconds: every,
			Installed:    installed[strings.ToLower(m.Name)],
		}
		t.Permissions.Shell = m.Permissions.Shell
		t.Permissions.Network = m.Permissions.Network
		t.Permissions.Git = m.Permissions.Git
		out = append(out, t)
		return nil
	})
	return out, err
}