package codemap

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// flushEvery bounds how many incremental writes accumulate before the index
// is written back to disk. Full rebuilds always flush.
const flushEvery = 30

// recheckAfter throttles the stat-walk that catches edits made outside the
// agent (editor saves, git pull).
const recheckAfter = 5 * time.Second

// Service owns one Index per project root, cached in memory and persisted
// under dir (the daemon data dir, never the project itself).
type Service struct {
	dir      string
	MaxFiles int

	mu      sync.Mutex
	indexes map[string]*Index
	pending map[string]int
}

func NewService(dir string) *Service {
	return &Service{dir: dir, MaxFiles: DefaultMaxFiles, indexes: map[string]*Index{}, pending: map[string]int{}}
}

var ErrRefused = errors.New("codemap: refusing to index this directory")

// RefuseReason blocks $HOME and / — indexing either is never what anyone meant.
func RefuseReason(root string) error {
	if root == "" {
		return errors.New("codemap: no workspace")
	}
	clean := filepath.Clean(root)
	if clean == string(filepath.Separator) {
		return ErrRefused
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == clean {
		return ErrRefused
	}
	if fi, err := os.Stat(clean); err != nil || !fi.IsDir() {
		return errors.New("codemap: workspace is not a directory: " + root)
	}
	return nil
}

func (s *Service) file(root string) string {
	sum := sha1.Sum([]byte(root))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8])+".json.gz")
}

// With runs fn on an up-to-date index for root under the service lock.
func (s *Service) With(root string, fn func(ix *Index) error) error {
	root, err := normRoot(root)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ix, err := s.ensure(root)
	if err != nil {
		return err
	}
	return fn(ix)
}

func (s *Service) ensure(root string) (*Index, error) {
	ix := s.indexes[root]
	if ix == nil {
		if loaded, err := loadIndexFile(s.file(root)); err == nil && loaded.Schema == SchemaVersion && loaded.Root == root {
			ix = loaded
		} else {
			ix = newIndex(root)
		}
		s.indexes[root] = ix
	}
	if !ix.wired || time.Since(ix.checkedAt) > recheckAfter {
		st := ix.scan(nil, s.maxFiles())
		if st.Added+st.Updated+st.Removed > 0 {
			_ = saveIndexFile(s.file(root), ix)
			s.pending[root] = 0
		}
	}
	return ix, nil
}

// Rebuild rescans root. full=true discards the cache and reparses every file.
func (s *Service) Rebuild(root string, full bool) (ScanStats, error) {
	root, err := normRoot(root)
	if err != nil {
		return ScanStats{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ix := s.indexes[root]
	if full || ix == nil {
		ix = newIndex(root)
		s.indexes[root] = ix
	}
	st := ix.scan(nil, s.maxFiles())
	s.pending[root] = 0
	return st, saveIndexFile(s.file(root), ix)
}

// Touch reparses only the given paths (absolute or root-relative). This is
// the hot path after an agent writes a file.
func (s *Service) Touch(root string, paths []string) (ScanStats, error) {
	root, err := normRoot(root)
	if err != nil {
		return ScanStats{}, err
	}
	if len(paths) == 0 {
		return ScanStats{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ix, err := s.ensure(root)
	if err != nil {
		return ScanStats{}, err
	}
	st := ix.scan(paths, s.maxFiles())
	s.pending[root] += len(st.Touched)
	if s.pending[root] >= flushEvery {
		s.pending[root] = 0
		_ = saveIndexFile(s.file(root), ix)
	}
	return st, nil
}

// Flush writes every dirty index to disk. Called on daemon shutdown.
func (s *Service) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for root, n := range s.pending {
		if n > 0 {
			if ix := s.indexes[root]; ix != nil {
				_ = saveIndexFile(s.file(root), ix)
			}
			s.pending[root] = 0
		}
	}
}

type Status struct {
	Root      string         `json:"root"`
	Files     int            `json:"files"`
	Edges     int            `json:"edges"`
	Symbols   int            `json:"symbols"`
	UpdatedAt time.Time      `json:"updatedAt"`
	ScanMS    int64          `json:"scanMs"`
	Truncated bool           `json:"truncated"`
	Langs     map[string]int `json:"langs"`
}

func (ix *Index) Status() Status {
	st := Status{Root: ix.Root, Files: len(ix.Files), Edges: len(ix.edges), UpdatedAt: ix.UpdatedAt, ScanMS: ix.ScanMS, Truncated: ix.Truncated, Langs: map[string]int{}}
	for _, rec := range ix.Files {
		st.Symbols += len(rec.Symbols)
		st.Langs[rec.Lang]++
	}
	return st
}

func (s *Service) maxFiles() int {
	if s.MaxFiles <= 0 {
		return DefaultMaxFiles
	}
	return s.MaxFiles
}

func normRoot(root string) (string, error) {
	if root == "" {
		return "", RefuseReason(root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if err := RefuseReason(abs); err != nil {
		return "", err
	}
	return abs, nil
}
