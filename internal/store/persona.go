package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// encodeFeatures stores nil as "" (use defaults) and an explicit empty list
// as "[]" (everything off) — the two mean different things.
func encodeFeatures(f []string) string {
	if f == nil {
		return ""
	}
	b, _ := json.Marshal(f)
	return string(b)
}

func decodeFeatures(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func (s *Store) GetSessionPersona(ctx context.Context, sessionID types.ID) (types.SessionPersona, error) {
	var p types.SessionPersona
	var features, updated string
	err := s.db.QueryRowContext(ctx, `SELECT session_id,character_id,profile_id,features,extra_prompt,updated_at FROM session_personas WHERE session_id=?`, sessionID).
		Scan(&p.SessionID, &p.CharacterID, &p.ProfileID, &features, &p.ExtraPrompt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Features = decodeFeatures(features)
	p.UpdatedAt = parseTS(updated)
	return p, err
}

func (s *Store) PutSessionPersona(ctx context.Context, p types.SessionPersona) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO session_personas(session_id,character_id,profile_id,features,extra_prompt,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET character_id=excluded.character_id, profile_id=excluded.profile_id,
		features=excluded.features, extra_prompt=excluded.extra_prompt, updated_at=excluded.updated_at`,
		p.SessionID, p.CharacterID, p.ProfileID, encodeFeatures(p.Features), p.ExtraPrompt, ts(time.Now().UTC()))
	return err
}

func (s *Store) DeleteSessionPersona(ctx context.Context, sessionID types.ID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_personas WHERE session_id=?`, sessionID)
	return err
}

func (s *Store) ListSessionPersonas(ctx context.Context) ([]types.SessionPersona, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id,character_id,profile_id,features,extra_prompt,updated_at FROM session_personas`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.SessionPersona
	for rows.Next() {
		var p types.SessionPersona
		var features, updated string
		if err := rows.Scan(&p.SessionID, &p.CharacterID, &p.ProfileID, &features, &p.ExtraPrompt, &updated); err != nil {
			return nil, err
		}
		p.Features = decodeFeatures(features)
		p.UpdatedAt = parseTS(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}
