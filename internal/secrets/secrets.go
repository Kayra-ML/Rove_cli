package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var ErrNotFound = errors.New("secrets: not found")

// Store keeps secrets encrypted at rest. The wrapping key is sourced from
// the OS keyring when available, otherwise from a 0600 file under the
// application data directory (never mixed with SQLite application rows).
type Store struct {
	mu      sync.Mutex
	dir     string
	gcm     cipher.AEAD
	mem     map[string][]byte // decrypted cache
	keyring Keyring
}

type Keyring interface {
	Get(service, user string) ([]byte, error)
	Set(service, user string, secret []byte) error
}

type fileKeyring struct{ path string }

func (f fileKeyring) Get(_, _ string) ([]byte, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (f fileKeyring) Set(_, _ string, secret []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f.path, secret, 0o600)
}

func Open(dataDir string, kr Keyring) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "secrets"), 0o700); err != nil {
		return nil, err
	}
	if kr == nil {
		kr = fileKeyring{path: filepath.Join(dataDir, "secrets", "master.key")}
	}
	master, err := loadOrCreateMaster(kr)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{dir: filepath.Join(dataDir, "secrets"), gcm: gcm, mem: map[string][]byte{}, keyring: kr}, nil
}

func loadOrCreateMaster(kr Keyring) ([]byte, error) {
	b, err := kr.Get("aether", "master")
	if err == nil && len(b) == 32 {
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := kr.Set("aether", "master", key); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Store) Put(id, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := s.gcm.Seal(nonce, nonce, []byte(value), []byte(id))
	path := s.blobPath(id)
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(ct)), 0o600); err != nil {
		return err
	}
	s.mem[id] = []byte(value)
	return nil
}

func (s *Store) Get(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.mem[id]; ok {
		return string(v), nil
	}
	b, err := os.ReadFile(s.blobPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(string(b))
	if err != nil {
		return "", err
	}
	ns := s.gcm.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("secrets: truncated blob")
	}
	pt, err := s.gcm.Open(nil, raw[:ns], raw[ns:], []byte(id))
	if err != nil {
		return "", err
	}
	s.mem[id] = pt
	return string(pt), nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mem, id)
	err := os.Remove(s.blobPath(id))
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

func (s *Store) blobPath(id string) string {
	safe := base64.RawURLEncoding.EncodeToString([]byte(id))
	return filepath.Join(s.dir, safe+".enc")
}

func PlatformHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain (fallback: 0600 master.key)"
	case "windows":
		return "Windows Credential Manager (fallback: 0600 master.key)"
	default:
		return "libsecret / 0600 master.key"
	}
}
