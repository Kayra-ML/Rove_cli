package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// Turn edits: the files a run changed and what they held before it, kept so
// the user can review a turn's changes and take any of them back.

// PutTurnEdit records a file's content before a run first changed it. Only
// the first write in a run counts (that is the state to go back to).
// It reports whether this was the run's first write to the file.
func (s *Store) PutTurnEdit(ctx context.Context, e types.TurnEdit) (bool, error) {
	r, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO turn_edits(run_id,session_id,workspace,path,before,existed,status,created_at) VALUES(?,?,?,?,?,?,'pending',?)`,
		e.RunID, e.SessionID, e.Workspace, e.Path, e.Before, boolInt(e.Existed), ts(time.Now().UTC()))
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// TurnEdits lists a run's edits; with no run, the chat's latest run that
// changed files.
func (s *Store) TurnEdits(ctx context.Context, sessionID, runID types.ID) ([]types.TurnEdit, error) {
	if runID == "" {
		if err := s.db.QueryRowContext(ctx, `SELECT run_id FROM turn_edits WHERE session_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, sessionID).Scan(&runID); err != nil {
			return nil, nil // nothing changed yet
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT run_id,session_id,workspace,path,before,existed,status,created_at FROM turn_edits WHERE session_id=? AND run_id=? ORDER BY created_at, rowid`, sessionID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.TurnEdit
	for rows.Next() {
		var e types.TurnEdit
		var existed int
		var created string
		if err := rows.Scan(&e.RunID, &e.SessionID, &e.Workspace, &e.Path, &e.Before, &existed, &e.Status, &created); err != nil {
			return nil, err
		}
		e.Existed, e.CreatedAt = existed == 1, parseTS(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetTurnEditStatus(ctx context.Context, runID types.ID, path, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE turn_edits SET status=? WHERE run_id=? AND path=?`, status, runID, path)
	return err
}

// Session effort: how hard a chat's reasoning model should think.

func (s *Store) GetSessionEffort(ctx context.Context, sessionID types.ID) string {
	var e string
	_ = s.db.QueryRowContext(ctx, `SELECT effort FROM session_effort WHERE session_id=?`, sessionID).Scan(&e)
	return e
}

// SetSessionEffort sets it; empty goes back to the model's own default.
func (s *Store) SetSessionEffort(ctx context.Context, sessionID types.ID, effort string) error {
	if effort == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM session_effort WHERE session_id=?`, sessionID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO session_effort(session_id,effort) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET effort=excluded.effort`, sessionID, effort)
	return err
}

// Terminal layouts: how a session's terminal mode is split and what each
// pane holds, kept with the session (so it opens the same anywhere).

func (s *Store) GetTerminalLayout(ctx context.Context, sessionID types.ID) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM terminal_layouts WHERE session_id=?`, sessionID).Scan(&body)
	if err != nil {
		return "", ErrNotFound
	}
	return body, nil
}

func (s *Store) PutTerminalLayout(ctx context.Context, sessionID types.ID, body string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO terminal_layouts(session_id,body) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET body=excluded.body`, sessionID, body)
	return err
}

// TurnEditPaths is the files a session's last run (or runID) changed, in the
// order it changed them: TurnEdits without the saved contents.
func (s *Store) TurnEditPaths(ctx context.Context, sessionID, runID types.ID) ([]string, error) {
	if runID == "" {
		if err := s.db.QueryRowContext(ctx, `SELECT run_id FROM turn_edits WHERE session_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, sessionID).Scan(&runID); err != nil {
			return nil, nil
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM turn_edits WHERE session_id=? AND run_id=? ORDER BY created_at, rowid`, sessionID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LastUserMessage is the content of the last thing a session was asked.
func (s *Store) LastUserMessage(ctx context.Context, sessionID types.ID) (string, error) {
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT content FROM messages WHERE session_id=? AND role=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, sessionID, types.RoleUser).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return content, err
}
