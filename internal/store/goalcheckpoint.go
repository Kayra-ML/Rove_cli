package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// A goal's checkpoint (where it stopped and what it had done) is kept as a
// JSON body so a goal can resume after a restart or a /stop.

func (s *Store) PutGoalCheckpoint(ctx context.Context, goalID types.ID, body string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO goal_checkpoints(goal_id,body,updated_at) VALUES(?,?,?)
		ON CONFLICT(goal_id) DO UPDATE SET body=excluded.body, updated_at=excluded.updated_at`,
		goalID, body, ts(time.Now().UTC()))
	return err
}

func (s *Store) GetGoalCheckpoint(ctx context.Context, goalID types.ID) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM goal_checkpoints WHERE goal_id=?`, goalID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return body, err
}

func (s *Store) DeleteGoalCheckpoint(ctx context.Context, goalID types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM goal_checkpoints WHERE goal_id=?`, goalID)
	return err
}
