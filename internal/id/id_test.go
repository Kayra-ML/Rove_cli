package id

import (
	"bytes"
	"strings"
	"testing"
)

func TestNew_Unique(t *testing.T) {
	g := New()
	seen := map[string]struct{}{}
	for i := 0; i < 256; i++ {
		v := string(g.New())
		if _, ok := seen[v]; ok {
			t.Fatalf("duplicate id %s", v)
		}
		seen[v] = struct{}{}
		if len(v) < 16 {
			t.Fatalf("id too short: %s", v)
		}
	}
}

func TestNewWithReader_DeterministicPrefix(t *testing.T) {
	r := bytes.NewReader(bytes.Repeat([]byte{0xab}, 64))
	g := NewWithReader(r)
	a := string(g.New())
	if !strings.HasPrefix(a, "abababababababababab") {
		t.Fatalf("unexpected id %s", a)
	}
}
