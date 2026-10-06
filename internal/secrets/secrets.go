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
	"sync"
	"testing"
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
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
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
		kr = defaultKeyring(dataDir)
	}
	master, err := loadOrCreateMaster(kr)
	if mk, ok := kr.(movingKeyring); ok && err != nil && !hasBlobs(filepath.Join(dataDir, "secrets")) {
		// nothing saved yet, so nothing to lose: start with a key in the file
		kr = mk.file
		master, err = loadOrCreateMaster(kr)
	}
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
	for _, service := range []string{"rovecode", "aether"} {
		b, err := kr.Get(service, "master")
		if err == nil && len(b) == 32 {
			return b, nil
		}
		// a key store that could not be read (a locked keychain) is not an
		// empty one: making a new key would lose every saved secret
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := kr.Set("rovecode", "master", key); err != nil {
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

func (s *Store) blobPath(id string) string {
	safe := base64.RawURLEncoding.EncodeToString([]byte(id))
	return filepath.Join(s.dir, safe+".enc")
}

// defaultKeyring is the system key store (the macOS keychain) holding the
// key, with the old 0600 file moved into it on first use. Tests, and
// ROVECODE_KEYRING=file, keep the file.
func defaultKeyring(dataDir string) Keyring {
	file := fileKeyring{path: filepath.Join(dataDir, "secrets", "master.key")}
	if testing.Testing() || os.Getenv("ROVECODE_KEYRING") == "file" {
		return file
	}
	sys := osKeyring(dataDir)
	if sys == nil {
		return file
	}
	return movingKeyring{sys: sys, file: file}
}

// movingKeyring keeps the key in the system store. A key still in the file
// (from before, or written there when the store could not be) is the one in
// use: it is moved into the store, and the file removed only once the store
// gives the same key back.
type movingKeyring struct {
	sys  Keyring
	file fileKeyring
}

func (m movingKeyring) Get(service, user string) ([]byte, error) {
	b, err := m.file.Get(service, user)
	if err == nil {
		if m.sys.Set(service, user, b) == nil {
			_ = os.Remove(m.file.path)
		}
		return b, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return m.sys.Get(service, user)
}

func (m movingKeyring) Set(service, user string, secret []byte) error {
	if err := m.sys.Set(service, user, secret); err != nil {
		return m.file.Set(service, user, secret)
	}
	return nil
}

func hasBlobs(dir string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, "*.enc"))
	return len(m) > 0
}
