package secrets

import (
	"errors"
	"testing"
)

func TestPutGetDelete(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("openai", "sk-test-123"); err != nil {
		t.Fatal(err)
	}
	v, err := s.Get("openai")
	if err != nil || v != "sk-test-123" {
		t.Fatalf("got %q %v", v, err)
	}
	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err = s2.Get("openai")
	if err != nil || v != "sk-test-123" {
		t.Fatalf("reload got %q %v", v, err)
	}
	if err := s.Delete("openai"); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get("openai")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestTamperFails(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("k", "v"); err != nil {
		t.Fatal(err)
	}
	path := s.blobPath("k")
	if err := osWriteCorrupt(path); err != nil {
		t.Fatal(err)
	}
	s.mem = map[string][]byte{}
	if _, err := s.Get("k"); err == nil {
		t.Fatal("expected decrypt error")
	}
}
