package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestClipKeepsHeadAndTail(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteString("satır ğüşiöç line\n")
	}
	s := "START\n" + b.String() + "END\n"
	out := Clip(s, 2000)
	if len(out) > 2000+300 {
		t.Fatalf("clipped to %d bytes", len(out))
	}
	if !strings.HasPrefix(out, "START\n") || !strings.HasSuffix(out, "END\n") {
		t.Fatalf("lost head or tail: %q … %q", out[:20], out[len(out)-20:])
	}
	if !strings.Contains(out, "cut from the middle") {
		t.Fatal("no note")
	}
	if !utf8.ValidString(out) {
		t.Fatal("split a rune")
	}
	if Clip("short", 2000) != "short" {
		t.Fatal("short text changed")
	}
}

func TestCapWriterBoundsMemory(t *testing.T) {
	w := newCapWriter(4000)
	line := []byte("ğğğğ build output line\n")
	w.Write([]byte("FIRST\n"))
	for i := 0; i < 100000; i++ {
		w.Write(line)
	}
	w.Write([]byte("LAST\n"))
	if cap(w.tail) > 3*4000 {
		t.Fatalf("tail grew to %d", cap(w.tail))
	}
	out := w.String()
	if len(out) > 4000 {
		t.Fatalf("String is %d bytes", len(out))
	}
	if !strings.HasPrefix(out, "FIRST\n") || !strings.HasSuffix(out, "LAST\n") || !utf8.ValidString(out) {
		t.Fatalf("bad output: %q", out)
	}
	// short output is kept whole
	s := newCapWriter(4000)
	s.Write([]byte("a\n"))
	s.Write([]byte("b\n"))
	if s.String() != "a\nb\n" {
		t.Fatalf("%q", s.String())
	}
}

func TestReadFileBinary(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.bin"), []byte{0x7f, 'E', 'L', 'F', 0, 1, 2}, 0o644)
	res, err := ReadFile{}.Call(context.Background(), Context{Workspace: dir}, json.RawMessage(`{"path":"x.bin"}`))
	if err != nil || !strings.Contains(res.Content, "binary file") {
		t.Fatalf("%v %q", err, res.Content)
	}
}

func TestShellOutputCapped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	res, err := Shell{}.Call(context.Background(), Context{Workspace: t.TempDir()}, json.RawMessage(`{"command":"echo first; yes line | head -n 200000; echo last"}`))
	if err != nil || res.IsError {
		t.Fatalf("%v %q", err, res.Content[:200])
	}
	if len(res.Content) > MaxOutput {
		t.Fatalf("output %d bytes", len(res.Content))
	}
	if !strings.HasPrefix(res.Content, "first") || !strings.Contains(res.Content, "last") {
		t.Fatal("lost head or tail")
	}
}

func TestShellBackgroundChildDoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	start := time.Now()
	res, err := Shell{}.Call(context.Background(), Context{Workspace: t.TempDir()}, json.RawMessage(`{"command":"echo up; sleep 30 &"}`))
	if err != nil || res.IsError {
		t.Fatalf("%v %q", err, res.Content)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("waited %s on a background child", d)
	}
	if !strings.HasPrefix(res.Content, "up") {
		t.Fatalf("%q", res.Content)
	}
}

func TestShellTimeoutKillsGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	start := time.Now()
	res, _ := Shell{Timeout: 500 * time.Millisecond}.Call(context.Background(), Context{Workspace: t.TempDir()}, json.RawMessage(`{"command":"sleep 30 | cat"}`))
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %s", d)
	}
	if !res.IsError || !strings.Contains(res.Content, "stopped after") {
		t.Fatalf("%q", res.Content)
	}
}
