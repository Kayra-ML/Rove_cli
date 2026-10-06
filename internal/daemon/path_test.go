package daemon

import "testing"

func TestJoinPathAndBetween(t *testing.T) {
	if got := joinPath([]string{"/a", "", "rel", "/b", "/a", " /c "}); got != "/a:/b:/c" {
		t.Fatalf("joinPath = %q", got)
	}
	if got := between("banner\n"+pathMark+"/x:/y"+pathMark+"bye", pathMark); got != "/x:/y" {
		t.Fatalf("between = %q", got)
	}
	if between("no marks", pathMark) != "" {
		t.Fatal("between without marks")
	}
}
