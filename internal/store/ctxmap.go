package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

const linkCols = `id,session_a,session_b,label,direction,auto,mode,color,created_at`

func scanLinks(rows *sql.Rows) ([]types.SessionLink, error) {
	defer rows.Close()
	var out []types.SessionLink
	for rows.Next() {
		var l types.SessionLink
		var created string
		var auto int
		if err := rows.Scan(&l.ID, &l.SessionA, &l.SessionB, &l.Label, &l.Direction, &auto, &l.Mode, &l.Color, &created); err != nil {
			return nil, err
		}
		l.Auto = auto != 0
		if l.Direction == "" {
			l.Direction = types.LinkBoth
		}
		if l.Mode == "" {
			l.Mode = types.LinkSmart
		}
		l.CreatedAt = parseTS(created)
		out = append(out, l)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) ListAllSessionLinks(ctx context.Context) ([]types.SessionLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+linkCols+` FROM session_links ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return scanLinks(rows)
}

func (s *Store) GetSessionLink(ctx context.Context, linkID types.ID) (types.SessionLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+linkCols+` FROM session_links WHERE id=?`, linkID)
	if err != nil {
		return types.SessionLink{}, err
	}
	out, err := scanLinks(rows)
	if err != nil {
		return types.SessionLink{}, err
	}
	if len(out) == 0 {
		return types.SessionLink{}, ErrNotFound
	}
	return out[0], nil
}

func (s *Store) UpdateSessionLink(ctx context.Context, l types.SessionLink) error {
	_, err := s.db.ExecContext(ctx, `UPDATE session_links SET label=?, direction=?, auto=?, mode=?, color=? WHERE id=?`,
		l.Label, l.Direction, boolInt(l.Auto), l.Mode, l.Color, l.ID)
	return err
}

func (s *Store) ListMapNodes(ctx context.Context) ([]types.MapNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id,x,y,updated_at FROM context_map_nodes ORDER BY updated_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.MapNode
	for rows.Next() {
		var n types.MapNode
		var updated string
		if err := rows.Scan(&n.SessionID, &n.X, &n.Y, &updated); err != nil {
			return nil, err
		}
		n.UpdatedAt = parseTS(updated)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) PutMapNode(ctx context.Context, n types.MapNode) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO context_map_nodes(session_id,x,y,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET x=excluded.x, y=excluded.y, updated_at=excluded.updated_at`,
		n.SessionID, n.X, n.Y, ts(time.Now().UTC()))
	return err
}

// DeleteMapNode takes a session off the canvas together with its cables.
func (s *Store) DeleteMapNode(ctx context.Context, sessionID types.ID) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM session_links WHERE session_a=? OR session_b=?`, sessionID, sessionID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM context_map_nodes WHERE session_id=?`, sessionID)
	return err
}

func (s *Store) PutRelay(ctx context.Context, r types.Relay) (types.Relay, error) {
	now := time.Now().UTC()
	if r.ID == "" {
		r.ID = id.NewID()
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	files, _ := json.Marshal(r.Files)
	matches, _ := json.Marshal(r.Matches)
	_, err := s.db.ExecContext(ctx, `INSERT INTO context_relays(id,link_id,from_session,to_session,kind,status,hop,files,summary,error,matches,tokens,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, error=excluded.error, summary=excluded.summary,
		files=excluded.files, matches=excluded.matches, tokens=excluded.tokens, updated_at=excluded.updated_at`,
		r.ID, r.LinkID, r.From, r.To, r.Kind, r.Status, r.Hop, string(files), r.Summary, r.Error, string(matches), r.Tokens, ts(r.CreatedAt), ts(r.UpdatedAt))
	return r, err
}

func (s *Store) ListRelays(ctx context.Context, limit int) ([]types.Relay, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,link_id,from_session,to_session,kind,status,hop,files,summary,error,matches,tokens,created_at,updated_at
		FROM context_relays ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Relay
	for rows.Next() {
		var r types.Relay
		var files, matches, created, updated string
		if err := rows.Scan(&r.ID, &r.LinkID, &r.From, &r.To, &r.Kind, &r.Status, &r.Hop, &files, &r.Summary, &r.Error, &matches, &r.Tokens, &created, &updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(files), &r.Files)
		_ = json.Unmarshal([]byte(matches), &r.Matches)
		r.CreatedAt, r.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, r)
	}
	return out, rows.Err()
}
