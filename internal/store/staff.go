package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// Work given to Office agents is stored as JSON bodies, like Teamwork plans:
// the task's shape belongs to the staff package, and a task is always read
// and written whole.

func (s *Store) PutStaffTask(ctx context.Context, id, profileID, sessionID types.ID, body string, created time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO staff_tasks(id,profile_id,session_id,body,created_at,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id, body=excluded.body, updated_at=excluded.updated_at`,
		id, profileID, sessionID, body, ts(created), ts(time.Now().UTC()))
	return err
}

func (s *Store) GetStaffTask(ctx context.Context, id types.ID) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM staff_tasks WHERE id=?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return body, err
}

// StaffTaskBySession finds the task a session was opened for.
func (s *Store) StaffTaskBySession(ctx context.Context, sessionID types.ID) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM staff_tasks WHERE session_id=? ORDER BY created_at DESC LIMIT 1`, sessionID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return body, err
}

// ListStaffTasks is an agent's tasks, newest first; an empty profile lists
// everyone's. limit <= 0 lists them all.
func (s *Store) ListStaffTasks(ctx context.Context, profileID types.ID, limit int) ([]string, error) {
	q := `SELECT body FROM staff_tasks`
	var args []any
	if profileID != "" {
		q += ` WHERE profile_id=?`
		args = append(args, profileID)
	}
	q += ` ORDER BY created_at DESC, rowid DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteStaffTasks drops an agent's tasks (the agent was removed).
func (s *Store) DeleteStaffTasks(ctx context.Context, profileID types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_tasks WHERE profile_id=?`, profileID)
	return err
}

// Watches: an Agent-space agent watching a chat on the Session Map. Stored
// as JSON bodies too; the session and profile columns are for lookups.

func (s *Store) PutStaffWatch(ctx context.Context, id, sessionID, profileID types.ID, body string, created time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO staff_watches(id,session_id,profile_id,body,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id, profile_id=excluded.profile_id, body=excluded.body`,
		id, sessionID, profileID, body, ts(created))
	return err
}

// ListStaffWatches lists the watches on one session, or every watch when
// sessionID is empty, oldest first.
func (s *Store) ListStaffWatches(ctx context.Context, sessionID types.ID) ([]string, error) {
	q := `SELECT body FROM staff_watches`
	var args []any
	if sessionID != "" {
		q += ` WHERE session_id=?`
		args = append(args, sessionID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at, rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteStaffWatch(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_watches WHERE id=?`, id)
	return err
}

// DeleteStaffWatchesOf drops the watches of a removed agent.
func (s *Store) DeleteStaffWatchesOf(ctx context.Context, profileID types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_watches WHERE profile_id=?`, profileID)
	return err
}

// Schedules: a task an agent is given on a timer.

func (s *Store) PutStaffSchedule(ctx context.Context, id, profileID types.ID, body string, created time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO staff_schedules(id,profile_id,body,created_at) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET profile_id=excluded.profile_id, body=excluded.body`,
		id, profileID, body, ts(created))
	return err
}

func (s *Store) ListStaffSchedules(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM staff_schedules ORDER BY created_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteStaffSchedule(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_schedules WHERE id=?`, id)
	return err
}

// DeleteStaffSchedulesOf drops a removed agent's schedules.
func (s *Store) DeleteStaffSchedulesOf(ctx context.Context, profileID types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_schedules WHERE profile_id=?`, profileID)
	return err
}

// Monitors: a command checked on a timer that gives an agent a task when
// it shows something new.

func (s *Store) PutStaffMonitor(ctx context.Context, id, profileID types.ID, body string, created time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO staff_monitors(id,profile_id,body,created_at) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET profile_id=excluded.profile_id, body=excluded.body`,
		id, profileID, body, ts(created))
	return err
}

func (s *Store) ListStaffMonitors(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM staff_monitors ORDER BY created_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteStaffMonitor(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_monitors WHERE id=?`, id)
	return err
}

// DeleteStaffMonitorsOf drops a removed agent's monitors.
func (s *Store) DeleteStaffMonitorsOf(ctx context.Context, profileID types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_monitors WHERE profile_id=?`, profileID)
	return err
}

// Handoffs: one agent's finished work handed on to another.

func (s *Store) PutStaffHandoff(ctx context.Context, id, fromID, toID types.ID, body string, created time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO staff_handoffs(id,from_id,to_id,body,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET from_id=excluded.from_id, to_id=excluded.to_id, body=excluded.body`,
		id, fromID, toID, body, ts(created))
	return err
}

func (s *Store) ListStaffHandoffs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM staff_handoffs ORDER BY created_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteStaffHandoff(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_handoffs WHERE id=?`, id)
	return err
}

// DeleteStaffHandoffsOf drops the handoffs a removed agent was in.
func (s *Store) DeleteStaffHandoffsOf(ctx context.Context, profileID types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM staff_handoffs WHERE from_id=? OR to_id=?`, profileID, profileID)
	return err
}
