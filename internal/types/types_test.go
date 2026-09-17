package types

import "testing"

func TestValidColumn(t *testing.T) {
	for _, c := range []KanbanColumn{ColBacklog, ColReady, ColRunning, ColReview, ColDone, ColBlocked} {
		if !ValidColumn(c) {
			t.Fatalf("expected %s valid", c)
		}
	}
	if ValidColumn("todo") {
		t.Fatal("todo must not be a valid column")
	}
}

func TestSkillPermissions_DangerousCapabilities(t *testing.T) {
	p := SkillPermissions{Filesystem: true, Shell: true, Dangerous: []string{"raw-socket"}}
	got := p.DangerousCapabilities()
	want := map[string]bool{"filesystem": true, "shell": true, "raw-socket": true}
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected %s", g)
		}
	}
}

func TestID_IsZero(t *testing.T) {
	var z ID
	if !z.IsZero() {
		t.Fatal("empty id should be zero")
	}
	if ID("abc").IsZero() {
		t.Fatal("non-empty should not be zero")
	}
}
