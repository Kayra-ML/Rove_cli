package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/persona"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func (s *Server) handlePersona(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	a := s.app
	var raw map[string]json.RawMessage
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &raw); err != nil {
			return nil, err
		}
	}
	str := func(k string) string {
		var v string
		_ = json.Unmarshal(raw[k], &v)
		return v
	}
	sid := types.ID(str("sessionId"))

	switch req.Method {
	case protocol.MethodPersonaCatalog:
		return core.MustJSON(map[string]any{"characters": persona.Characters, "features": persona.Features, "defaults": persona.DefaultFeatures}), nil

	case protocol.MethodPersonaBadges:
		// One call for every session list: who each session is, without
		// resolving personas one by one.
		type badge struct {
			Name string `json:"name"`
			// CharacterID groups a session under its agent in the sidebar;
			// an office agent (a profile) groups by ProfileID and is drawn
			// with its Mark and Color.
			CharacterID string `json:"characterId,omitempty"`
			ProfileID   string `json:"profileId,omitempty"`
			Mark        string `json:"mark,omitempty"`
			Color       string `json:"color,omitempty"`
		}
		profiles := map[types.ID]types.AgentProfile{}
		if list, err := a.Store.ListAgentProfiles(ctx); err == nil {
			for _, p := range list {
				profiles[p.ID] = p
			}
		}
		profileBadge := func(p types.AgentProfile) badge {
			return badge{Name: p.Name, CharacterID: p.CharacterID, ProfileID: string(p.ID), Mark: p.Mark, Color: p.Color}
		}
		out := map[string]badge{}
		sps, err := a.Store.ListSessionPersonas(ctx)
		if err != nil {
			return nil, err
		}
		for _, sp := range sps {
			switch {
			case sp.ProfileID != "":
				if p, ok := profiles[sp.ProfileID]; ok {
					out[string(sp.SessionID)] = profileBadge(p)
				}
			case sp.CharacterID != "":
				if c, ok := persona.CharacterByID(sp.CharacterID); ok {
					out[string(sp.SessionID)] = badge{Name: c.Name, CharacterID: c.ID}
				}
			default:
				// own settings without a character: the default profile does
				// not apply, so no badge either
				out[string(sp.SessionID)] = badge{}
			}
		}
		var def *badge
		if p, err := a.Store.GetDefaultProfile(ctx); err == nil && p != nil {
			b := profileBadge(*p)
			def = &b
		}
		return core.MustJSON(map[string]any{"sessions": out, "default": def}), nil

	case protocol.MethodSessionCancel:
		// /stop stops the chat's goal too, not just its current turn
		goals := 0
		if a.Goals != nil {
			goals = a.Goals.CancelSession(sid)
		}
		a.Agents.CancelSession(sid)
		return core.MustJSON(map[string]any{"ok": true, "goals": goals}), nil

	case protocol.MethodPersonaClear:
		err := a.Store.DeleteSessionPersona(ctx, sid)
		if err != nil {
			return nil, err
		}
		return s.personaView(ctx, sid)

	case protocol.MethodPersonaGet:
		return s.personaView(ctx, sid)

	case protocol.MethodPersonaSet:
		if sid == "" {
			return nil, fmt.Errorf("sessionId required")
		}
		if _, err := a.Store.GetSession(ctx, sid); err != nil {
			return nil, fmt.Errorf("session not found")
		}
		sp, err := a.Store.GetSessionPersona(ctx, sid)
		if err != nil && err != store.ErrNotFound {
			return nil, err
		}
		sp.SessionID = sid
		// Picking a character or a profile starts from its own defaults;
		// features sent in the same call still win.
		if _, ok := raw["characterId"]; ok {
			id := str("characterId")
			if id != "" {
				c, ok := persona.CharacterByID(id)
				if !ok {
					if c, ok = persona.Match(id); !ok {
						return nil, fmt.Errorf("unknown character %q", id)
					}
				}
				id = c.ID
			}
			sp.CharacterID, sp.ProfileID, sp.Features = id, "", nil
		}
		if _, ok := raw["profileId"]; ok {
			id := types.ID(str("profileId"))
			if id != "" {
				if _, err := a.Store.GetAgentProfile(ctx, id); err != nil {
					return nil, fmt.Errorf("profile not found")
				}
			}
			sp.ProfileID, sp.CharacterID, sp.Features = id, "", nil
		}
		if f, ok := raw["features"]; ok {
			if string(f) == "null" {
				sp.Features = nil
			} else {
				var list []string
				if err := json.Unmarshal(f, &list); err != nil {
					return nil, err
				}
				sp.Features = cleanFeatures(list)
			}
		}
		if _, ok := raw["extraPrompt"]; ok {
			sp.ExtraPrompt = str("extraPrompt")
		}
		if err := a.Store.PutSessionPersona(ctx, sp); err != nil {
			return nil, err
		}
		return s.personaView(ctx, sid)
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}

// personaView is the resolved persona plus what it costs per request.
func (s *Server) personaView(ctx context.Context, sid types.ID) (json.RawMessage, error) {
	r, err := persona.Resolve(ctx, s.app.Store, sid)
	if err != nil {
		return nil, err
	}
	cost := r.Cost(s.app.Tools.Specs())
	if r.Features == nil {
		r.Features = []string{}
		for _, f := range persona.Features {
			if f.Group == "tools" {
				r.Features = append(r.Features, f.Key)
			}
		}
	}
	return core.MustJSON(map[string]any{"persona": r, "cost": cost}), nil
}

func cleanFeatures(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, k := range in {
		if _, ok := persona.FeatureByKey(k); ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
