package index

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// Result is a single FTS5 search hit.
type Result struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
	Rank    float64 `json:"rank"`
}

// Indexer manages an in-process SQLite FTS5 index for a workspace.
type Indexer struct {
	mu      sync.RWMutex
	db      *sql.DB
	dbPath  string
	indexed bool
}

// New opens (or creates) an FTS5 index at dbPath.
func New(dbPath string) (*Indexer, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS codebase_fts USING fts5(path UNINDEXED, content, tokenize='porter ascii')`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("index: create fts5 table: %w", err)
	}
	return &Indexer{db: db, dbPath: dbPath}, nil
}

// Close closes the underlying database.
func (idx *Indexer) Close() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.db != nil {
		return idx.db.Close()
	}
	return nil
}

// IsIndexed reports whether a full build has completed at least once.
func (idx *Indexer) IsIndexed() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.indexed
}

// skipDir returns true for directories that should be excluded from indexing.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", ".next", "vendor", ".aether", "__pycache__", ".cache":
		return true
	}
	return false
}

// skipFile returns true for files that should not be indexed.
func skipFile(name string) bool {
	// Skip binary-likely extensions and lock files.
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp",
		".woff", ".woff2", ".ttf", ".eot",
		".zip", ".tar", ".gz", ".br",
		".lock", ".sum",
		".bin", ".exe", ".dll", ".so", ".dylib",
		".pdf", ".db", ".sqlite", ".sqlite3":
		return true
	}
	// Skip specific filenames.
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum":
		return true
	}
	return false
}

// IndexWorkspace walks root and rebuilds the FTS5 index.
// It runs inside a single transaction for atomicity and performance.
func (idx *Indexer) IndexWorkspace(ctx context.Context, root string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Clear existing index.
	if _, err := tx.ExecContext(ctx, `DELETE FROM codebase_fts`); err != nil {
		return fmt.Errorf("index: clear: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO codebase_fts(path, content) VALUES(?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	count := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil // skip unreadable paths
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFile(d.Name()) {
			return nil
		}
		// Skip files larger than 512 KB.
		info, err := d.Info()
		if err != nil || info.Size() > 512*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if _, err := stmt.ExecContext(ctx, rel, string(data)); err != nil {
			return nil // non-fatal
		}
		count++
		return nil
	})
	if err != nil {
		return fmt.Errorf("index: walk: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("index: commit: %w", err)
	}
	idx.indexed = true
	return nil
}

// Search queries the FTS5 index and returns up to limit results.
func (idx *Indexer) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}
	// FTS5 snippet() extracts a short excerpt.
	rows, err := idx.db.QueryContext(ctx,
		`SELECT path, snippet(codebase_fts, 1, '[', ']', '...', 20), rank
		 FROM codebase_fts
		 WHERE codebase_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit)
	if err != nil {
		return nil, fmt.Errorf("index: search: %w", err)
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Path, &r.Snippet, &r.Rank); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}