package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
	"gopkg.in/yaml.v3"
)

type Runtime struct {
	store  *store.Store
	skillDir string
}

func New(s *store.Store, dir string) *Runtime {
	return &Runtime{store: s, skillDir: dir}
}

func (r *Runtime) Dir() string { return r.skillDir }

func LoadManifest(path string) (types.SkillManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return types.SkillManifest{}, err
	}
	var m types.SkillManifest
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(b, &m); err != nil {
			return m, err
		}
	default:
		if err := json.Unmarshal(b, &m); err != nil {
			return m, err
		}
	}
	if m.Name == "" {
		return m, fmt.Errorf("skill manifest missing name")
	}
	if m.Version == "" {
		return m, fmt.Errorf("skill %s missing version", m.Name)
	}
	return m, nil
}

func (r *Runtime) InstallFromDir(ctxDir string) (types.InstalledSkill, error) {
	manPath := findManifest(ctxDir)
	if manPath == "" {
		return types.InstalledSkill{}, fmt.Errorf("no skill.yaml/json in %s", ctxDir)
	}
	m, err := LoadManifest(manPath)
	if err != nil {
		return types.InstalledSkill{}, err
	}
	dest := filepath.Join(r.skillDir, m.Name)
	if err := copyTree(ctxDir, dest); err != nil {
		return types.InstalledSkill{}, err
	}
	sk := types.InstalledSkill{Manifest: m, Path: dest, Source: "local", Enabled: true}
	if r.store != nil {
		if err := r.store.UpsertSkill(background(), sk); err != nil {
			return sk, err
		}
	}
	return sk, nil
}

func (r *Runtime) List() ([]types.InstalledSkill, error) {
	if r.store != nil {
		if items, err := r.store.ListSkills(background()); err == nil && len(items) > 0 {
			return items, nil
		}
	}
	return r.scanDisk()
}

func (r *Runtime) scanDisk() ([]types.InstalledSkill, error) {
	ents, err := os.ReadDir(r.skillDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []types.InstalledSkill
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(r.skillDir, e.Name())
		mp := findManifest(p)
		if mp == "" {
			continue
		}
		m, err := LoadManifest(mp)
		if err != nil {
			continue
		}
		out = append(out, types.InstalledSkill{Manifest: m, Path: p, Source: "local", Enabled: true})
	}
	return out, nil
}

func (r *Runtime) Get(name string) (types.InstalledSkill, error) {
	all, err := r.List()
	if err != nil {
		return types.InstalledSkill{}, err
	}
	for _, s := range all {
		if s.Manifest.Name == name {
			return s, nil
		}
	}
	return types.InstalledSkill{}, fmt.Errorf("skill not found: %s", name)
}

func (r *Runtime) SetEnabled(name string, enabled bool) (types.InstalledSkill, error) {
	sk, err := r.Get(name)
	if err != nil {
		return sk, err
	}
	sk.Enabled = enabled
	if r.store != nil {
		if err := r.store.UpsertSkill(background(), sk); err != nil {
			return sk, err
		}
	}
	return sk, nil
}

func (r *Runtime) Uninstall(name string) error {
	sk, err := r.Get(name)
	if err != nil {
		return err
	}
	if r.store != nil {
		_ = r.store.DeleteSkill(background(), name)
	}
	return os.RemoveAll(sk.Path)
}

func (r *Runtime) SlashCommands() []types.SlashCommand {
	all, _ := r.List()
	var out []types.SlashCommand
	for _, s := range all {
		if !s.Enabled {
			continue
		}
		out = append(out, s.Manifest.Commands...)
	}
	return out
}

func findManifest(dir string) string {
	for _, n := range []string{"skill.yaml", "skill.yml", "SKILL.md", "skill.json", "manifest.yaml"} {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			if n == "SKILL.md" {
				return p
			}
			return p
		}
	}
	return ""
}

func copyTree(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode())
	})
}
