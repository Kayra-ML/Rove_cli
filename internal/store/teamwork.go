package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// Teamwork plans are stored as JSON bodies: the plan's shape belongs to the
// teamwork package, and a plan is always read and written whole.

func (s *Store) PutTeamworkPlan(ctx context.Context, id, sessionID types.ID, body string, created time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO teamwork_plans(id,session_id,body,created_at,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET body=excluded.body, updated_at=excluded.updated_at`,
		id, sessionID, body, ts(created), ts(time.Now().UTC()))
	return err
}

func (s *Store) GetTeamworkPlan(ctx context.Context, id types.ID) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM teamwork_plans WHERE id=?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return body, err
}

// LatestTeamworkPlan is a chat's most recent plan.
func (s *Store) LatestTeamworkPlan(ctx context.Context, sessionID types.ID) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM teamwork_plans WHERE session_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, sessionID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return body, err
}

func (s *Store) DeleteTeamworkPlan(ctx context.Context, id types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM teamwork_plans WHERE id=?`, id)
	return err
}
