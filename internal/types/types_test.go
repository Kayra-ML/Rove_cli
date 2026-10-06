package types

import "testing"

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

func TestMCPSlug(t *testing.T) {
	for in, want := range map[string]string{"GitHub": "github", "My Echo": "my-echo", " a__b ": "a-b", "!!!": "server", "Şirket CRM": "sirket-crm", "Çağrı Ölçüm": "cagri-olcum"} {
		if got := MCPSlug(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
