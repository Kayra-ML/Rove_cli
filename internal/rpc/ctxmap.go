package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/ctxmap"
	"github.com/Kayra-ML/rove/internal/persona"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

type ctxParams struct {
	SessionID types.ID `json:"sessionId"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	ID        types.ID `json:"id"`
	SessionA  types.ID `json:"sessionA"`
	SessionB  types.ID `json:"sessionB"`
	Label     *string  `json:"label"`
	Direction *string  `json:"direction"`
	Auto      *bool    `json:"auto"`
	Mode      *string  `json:"mode"`
	Color     *string  `json:"color"`
	From      types.ID `json:"from"`
	To        types.ID `json:"to"`
	Content   string   `json:"content"`
	Limit     int      `json:"limit"`
}

func (s *Server) handleCtx(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	a := s.app
	var p ctxParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	switch req.Method {
	case protocol.MethodCtxGet:
		nodes, err := a.Store.ListMapNodes(ctx)
		if err != nil {
			return nil, err
		}
		links, err := a.Store.ListAllSessionLinks(ctx)
		if err != nil {
			return nil, err
		}
		if nodes == nil {
			nodes = []types.MapNode{}
		}
		if links == nil {
			links = []types.SessionLink{}
		}
		return core.MustJSON(map[string]any{"nodes": nodes, "links": links}), nil

	case protocol.MethodCtxPlace:
		if p.SessionID == "" {
			return nil, fmt.Errorf("sessionId required")
		}
		// an Agent-space agent stands on the map as "agent:<profile>"
		if pid, ok := strings.CutPrefix(string(p.SessionID), "agent:"); ok {
			if _, err := a.Store.GetAgentProfile(ctx, types.ID(pid)); err != nil {
				return nil, fmt.Errorf("agent not found")
			}
		} else if _, err := a.Store.GetSession(ctx, p.SessionID); err != nil {
			return nil, fmt.Errorf("session not found")
		}
		err := a.Store.PutMapNode(ctx, types.MapNode{SessionID: p.SessionID, X: p.X, Y: p.Y})
		s.mapChanged()
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodCtxRemove:
		err := a.Store.DeleteMapNode(ctx, p.SessionID)
		s.mapChanged()
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodCtxLink:
		if p.SessionA == "" || p.SessionB == "" || p.SessionA == p.SessionB {
			return nil, fmt.Errorf("a cable needs two different sessions")
		}
		// One cable per pair, whichever end it was drawn from.
		if existing, err := a.Store.ListSessionLinks(ctx, p.SessionA); err == nil {
			for _, l := range existing {
				if l.Other(p.SessionA) == p.SessionB {
					return core.MustJSON(l), nil
				}
			}
		}
		l := types.SessionLink{SessionA: p.SessionA, SessionB: p.SessionB, Direction: types.LinkBoth, Auto: true, Mode: types.LinkSmart}
		applyLinkEdits(&l, p)
		out, err := a.Store.CreateSessionLink(ctx, l)
		s.mapChanged()
		return core.MustJSON(out), err

	case protocol.MethodCtxUpdateLink:
		l, err := a.Store.GetSessionLink(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		applyLinkEdits(&l, p)
		if err := a.Store.UpdateSessionLink(ctx, l); err != nil {
			return nil, err
		}
		s.mapChanged()
		return core.MustJSON(l), nil

	case protocol.MethodCtxUnlink:
		err := a.Store.DeleteSessionLink(ctx, p.ID)
		s.mapChanged()
		return core.MustJSON(map[string]any{"ok": err == nil}), err

	case protocol.MethodCtxSend:
		r, err := a.Links.Send(ctx, p.From, p.To, p.Content, ctxmap.KindManual)
		return core.MustJSON(r), err

	case protocol.MethodCtxRelays:
		out, err := a.Store.ListRelays(ctx, p.Limit)
		if out == nil {
			out = []types.Relay{}
		}
		return core.MustJSON(out), err
	case protocol.MethodCtxAssistant:
		// one assistant per chat, kept: asking again picks the talk up
		parent, err := a.Sess.Get(ctx, p.SessionID)
		if err != nil {
			return nil, fmt.Errorf("session not found")
		}
		if parent.ParentID != "" {
			return nil, fmt.Errorf("the map's assistant belongs to a top-level chat")
		}
		kids, err := a.Sess.Children(ctx, parent.ID)
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			if k.Space == types.SpaceMap {
				s.inheritModel(ctx, parent.ID, k.ID)
				return core.MustJSON(k), nil
			}
		}
		out, err := a.Sess.CreateChildIn(ctx, parent, types.SpaceMap, "Bağlam asistanı · "+parent.Title)
		if err == nil {
			s.inheritModel(ctx, parent.ID, out.ID)
		}
		return core.MustJSON(out), err
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}

func applyLinkEdits(l *types.SessionLink, p ctxParams) {
	if p.Label != nil {
		l.Label = *p.Label
	}
	if p.Direction != nil {
		switch *p.Direction {
		case types.LinkBoth, types.LinkAToB, types.LinkBToA:
			l.Direction = *p.Direction
		}
	}
	if p.Auto != nil {
		l.Auto = *p.Auto
	}
	if p.Mode != nil {
		switch *p.Mode {
		case types.LinkSmart, types.LinkAlways:
			l.Mode = *p.Mode
		}
	}
	if p.Color != nil && types.ValidCableColor(*p.Color) {
		l.Color = *p.Color
	}
}

// mapChanged lets every open canvas refresh when another client edits it.
func (s *Server) mapChanged() {
	if s.app.Bus != nil {
		s.app.Bus.Publish(types.Event{Type: types.EventMapChanged, Topic: "ctxmap"})
	}
}

// inheritModel gives the map's assistant the model its chat talks to, until
// the user picks another for it: the agent's own default may point at a
// provider that is not set up, while the chat's model is known to work.
func (s *Server) inheritModel(ctx context.Context, chatID, helperID types.ID) {
	st := s.app.Store
	if m, err := st.GetSessionModel(ctx, helperID); err == nil && m.Model != "" {
		return
	}
	if m, err := st.GetSessionModel(ctx, chatID); err == nil && m.Model != "" {
		_ = st.SetSessionModel(ctx, helperID, m)
		return
	}
	if r, err := persona.Resolve(ctx, st, chatID); err == nil && r.Model != "" {
		_ = st.SetSessionModel(ctx, helperID, types.ModelRef{Provider: r.Provider, Model: r.Model})
	}
}
