package diffx

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	a := "package main\n\nfunc a() {}\n\nfunc b() {}\n"
	b := "package main\n\nfunc a() { return }\n\nfunc b() {}\nfunc c() {}\n"
	d, st := Unified("x.go", a, b)
	if st.Added != 2 || st.Removed != 1 {
		t.Fatalf("stats = %+v\n%s", st, d)
	}
	for _, want := range []string{"--- a/x.go", "+++ b/x.go", "@@ -1,5 +1,6 @@", "-func a() {}", "+func a() { return }", "+func c() {}", " func b() {}"} {
		if !strings.Contains(d, want) {
			t.Fatalf("missing %q in\n%s", want, d)
		}
	}
	if d, st := Unified("x", "same\n", "same\n"); d != "" || st.Added+st.Removed != 0 {
		t.Fatal("identical files have a diff")
	}
	// a new file is all additions; a deleted one all removals
	if _, st := Unified("n", "", "a\nb\n"); st.Added != 2 || st.Removed != 0 {
		t.Fatalf("new = %+v", st)
	}
	if _, st := Unified("n", "a\nb\n", ""); st.Removed != 2 {
		t.Fatalf("deleted = %+v", st)
	}
	// far-apart changes make separate hunks
	var x, y []string
	for i := 0; i < 40; i++ {
		x = append(x, "line")
		y = append(y, "line")
	}
	y[2], y[35] = "first", "second"
	d, _ = Unified("f", strings.Join(x, "\n"), strings.Join(y, "\n"))
	if strings.Count(d, "@@ ") != 2 {
		t.Fatalf("hunks:\n%s", d)
	}
}
