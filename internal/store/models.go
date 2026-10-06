package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// A chat's own model (picked with /models) wins over its profile's and its
// agent's. No row means the chat has none.

func (s *Store) GetSessionModel(ctx context.Context, sessionID types.ID) (types.ModelRef, error) {
	var m types.ModelRef
	err := s.db.QueryRowContext(ctx, `SELECT provider, model FROM session_models WHERE session_id=?`, sessionID).Scan(&m.Provider, &m.Model)
	if errors.Is(err, sql.ErrNoRows) {
		return types.ModelRef{}, ErrNotFound
	}
	return m, err
}

// SetSessionModel sets a chat's model; an empty model clears it.
func (s *Store) SetSessionModel(ctx context.Context, sessionID types.ID, m types.ModelRef) error {
	if m.Model == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM session_models WHERE session_id=?`, sessionID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO session_models(session_id,provider,model,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET provider=excluded.provider, model=excluded.model, updated_at=excluded.updated_at`,
		sessionID, m.Provider, m.Model, ts(time.Now().UTC()))
	return err
}
