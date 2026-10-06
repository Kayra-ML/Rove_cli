package secrets

import (
	"errors"
	"os"
	"path/filepath"
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

type memKeyring struct {
	m    map[string][]byte
	fail error
}

func (k *memKeyring) Get(service, user string) ([]byte, error) {
	if k.fail != nil {
		return nil, k.fail
	}
	if b, ok := k.m[service+"/"+user]; ok {
		return b, nil
	}
	return nil, ErrNotFound
}

func (k *memKeyring) Set(service, user string, b []byte) error {
	if k.fail != nil {
		return k.fail
	}
	k.m[service+"/"+user] = b
	return nil
}

func TestMovingKeyringMovesFileKey(t *testing.T) {
	dir := t.TempDir()
	file := fileKeyring{path: filepath.Join(dir, "secrets", "master.key")}
	s, err := Open(dir, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("k", "v"); err != nil {
		t.Fatal(err)
	}
	sys := &memKeyring{m: map[string][]byte{}}
	s2, err := Open(dir, movingKeyring{sys: sys, file: file})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := s2.Get("k"); err != nil || v != "v" {
		t.Fatalf("after move: %q %v", v, err)
	}
	if _, err := os.Stat(file.path); !os.IsNotExist(err) {
		t.Fatal("file key not removed after the move")
	}
	s3, err := Open(dir, movingKeyring{sys: sys, file: file})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := s3.Get("k"); err != nil || v != "v" {
		t.Fatalf("from the store: %q %v", v, err)
	}
}

func TestLockedKeyring(t *testing.T) {
	dir := t.TempDir()
	file := fileKeyring{path: filepath.Join(dir, "secrets", "master.key")}
	locked := &memKeyring{fail: errors.New("keychain locked")}
	// nothing saved yet: start with the file
	s, err := Open(dir, movingKeyring{sys: locked, file: file})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("k", "v"); err != nil {
		t.Fatal(err)
	}
	// the key moves into a working store
	sys := &memKeyring{m: map[string][]byte{}}
	if _, err := Open(dir, movingKeyring{sys: sys, file: file}); err != nil {
		t.Fatal(err)
	}
	// then that store locked, with secrets saved: no new key
	sys.fail = errors.New("keychain locked")
	if _, err := Open(dir, movingKeyring{sys: sys, file: file}); err == nil {
		t.Fatal("opened with a new key while the key store was unreadable")
	}
}
