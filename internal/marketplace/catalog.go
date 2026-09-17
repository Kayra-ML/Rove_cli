package marketplace

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aether-dev/aether/internal/skill"
	"github.com/aether-dev/aether/internal/types"
)

//go:embed all:bundled
var bundled embed.FS

// Catalog is a local registry designed so a future public registry can
// drop in versioning, signatures, ratings and compatibility without
// changing the Skill Runtime API.
type Catalog struct {
	root    string
	runtime *skill.Runtime
}

func New(root string, rt *skill.Runtime) *Catalog {
	c := &Catalog{root: root, runtime: rt}
	_ = c.SeedBundled()
	return c
}

func (c *Catalog) SeedBundled() error {
	if c.root == "" {
		return nil
	}
	existing, _ := c.List()
	have := map[string]bool{}
	for _, e := range existing {
		have[e.Manifest.Name] = true
	}
	seedOne := func(embedDir, kind, plugin, name string) error {
		if have[name] {
			return nil
		}
		tmp, err := materializeEmbedDir(embedDir)
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		ms, err := c.publishDir(tmp, "rovecode")
		if err != nil {
			return err
		}
		ms.Kind = kind
		ms.Plugin = plugin
		have[name] = true
		return c.upsertIndex(ms)
	}
	err := fs.WalkDir(bundled, "bundled/plugins", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "skill.yaml" {
			return err
		}
		plugin := filepath.Base(filepath.Dir(path))
		return seedOne(filepath.Dir(path), "plugin", plugin, plugin)
	})
	if err != nil {
		return err
	}
	return fs.WalkDir(bundled, "bundled/skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "skill.yaml" {
			return err
		}
		plugin := filepath.Base(filepath.Dir(filepath.Dir(path)))
		name := filepath.Base(filepath.Dir(path))
		return seedOne(filepath.Dir(path), "skill", plugin, name)
	})
}

func materializeEmbedDir(embedDir string) (string, error) {
	tmp, err := os.MkdirTemp("", "aether-rove-*")
	if err != nil {
		return "", err
	}
	err = fs.WalkDir(bundled, embedDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(embedDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := bundled.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	return tmp, err
}

type IndexEntry struct {
	types.MarketSkill
	Versions []string `json:"versions"`
}

func (c *Catalog) IndexPath() string { return filepath.Join(c.root, "index.json") }

func (c *Catalog) PublishLocal(dir string) (types.MarketSkill, error) {
	return c.publishDir(dir, "local-registry")
}

func (c *Catalog) publishDir(dir, source string) (types.MarketSkill, error) {
	mp := firstManifest(dir)
	if mp == "" {
		return types.MarketSkill{}, fmt.Errorf("no manifest in %s", dir)
	}
	m, err := skill.LoadManifest(mp)
	if err != nil {
		return types.MarketSkill{}, err
	}
	sum, err := hashDir(dir)
	if err != nil {
		return types.MarketSkill{}, err
	}
	dest := filepath.Join(c.root, m.Name, m.Version)
	if err := copyTree(dir, dest); err != nil {
		return types.MarketSkill{}, err
	}
	kind := "skill"
	plugin := ""
	if b, err := os.ReadFile(filepath.Join(dir, "market.json")); err == nil {
		var extra struct {
			Kind   string `json:"kind"`
			Plugin string `json:"plugin"`
		}
		if json.Unmarshal(b, &extra) == nil {
			if extra.Kind != "" {
				kind = extra.Kind
			}
			plugin = extra.Plugin
		}
	}
	ms := types.MarketSkill{
		Manifest: m,
		Source:   source,
		Signed:   false,
		Checksum: sum,
		Kind:     kind,
		Plugin:   plugin,
	}
	if err := c.upsertIndex(ms); err != nil {
		return ms, err
	}
	return ms, nil
}

func (c *Catalog) List() ([]IndexEntry, error) {
	b, err := os.ReadFile(c.IndexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx []IndexEntry
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, err
	}
	sort.Slice(idx, func(i, j int) bool { return idx[i].Manifest.Name < idx[j].Manifest.Name })
	return idx, nil
}

func (c *Catalog) Install(name, version string) (types.InstalledSkill, error) {
	src := filepath.Join(c.root, name, version)
	if _, err := os.Stat(src); err != nil {
		return types.InstalledSkill{}, fmt.Errorf("registry package not found: %s@%s", name, version)
	}
	return c.runtime.InstallFromDir(src)
}

func (c *Catalog) Search(q string) ([]IndexEntry, error) {
	all, err := c.List()
	if err != nil {
		return nil, err
	}
	if q == "" {
		return all, nil
	}
	ql := strings.ToLower(q)
	var out []IndexEntry
	for _, e := range all {
		blob := strings.ToLower(e.Manifest.Name + " " + e.Manifest.Description + " " + e.Manifest.Author + " " + e.Plugin + " " + e.Kind)
		if strings.Contains(blob, ql) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (c *Catalog) upsertIndex(ms types.MarketSkill) error {
	idx, _ := c.List()
	found := false
	for i := range idx {
		if idx[i].Manifest.Name == ms.Manifest.Name {
			idx[i].MarketSkill = ms
			idx[i].Versions = uniqueAppend(idx[i].Versions, ms.Manifest.Version)
			found = true
			break
		}
	}
	if !found {
		idx = append(idx, IndexEntry{MarketSkill: ms, Versions: []string{ms.Manifest.Version}})
	}
	if err := os.MkdirAll(c.root, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.IndexPath(), b, 0o644)
}

func uniqueAppend(in []string, v string) []string {
	for _, x := range in {
		if x == v {
			return in
		}
	}
	return append(in, v)
}

func firstManifest(dir string) string {
	for _, n := range []string{"skill.yaml", "skill.yml", "skill.json", "manifest.yaml"} {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func hashDir(root string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		h.Write([]byte(rel))
		h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func copyTree(src, dest string) error {
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
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode())
	})
}
