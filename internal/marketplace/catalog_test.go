package marketplace

import (
	"path/filepath"
	"testing"

	"github.com/Kayra-ML/rove/internal/skill"
)

func TestSeedBundledRoveCode(t *testing.T) {
	reg := t.TempDir()
	skills := filepath.Join(t.TempDir(), "skills")
	rt := skill.New(nil, skills)
	c := New(reg, rt)
	all, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	var plugins, skillN int
	have := map[string]bool{}
	for _, e := range all {
		have[e.Manifest.Name] = true
		switch e.Kind {
		case "plugin":
			plugins++
		default:
			skillN++
		}
	}
	if plugins != 11 {
		t.Fatalf("plugins %d want 11", plugins)
	}
	if skillN != 72 {
		t.Fatalf("skills %d want 72", skillN)
	}
	for _, n := range []string{"web-design", "layout-composition", "rust-core", "prompt-engineering"} {
		if !have[n] {
			t.Fatalf("missing %s", n)
		}
	}
	hits, err := c.Search("layout")
	if err != nil || len(hits) == 0 {
		t.Fatalf("search layout: %v %d", err, len(hits))
	}
	c2 := New(reg, rt)
	again, err := c2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(all) {
		t.Fatalf("reseed grew catalog %d -> %d", len(all), len(again))
	}
}
