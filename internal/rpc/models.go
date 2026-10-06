package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/persona"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// modelChoice is one model /models offers: a provider's configured model.
type modelChoice struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Default  bool   `json:"default,omitempty"` // the default provider's
}

// sessionModel is the model a chat runs on, and where that comes from:
// "chat" (picked with /models), "profile" or "agent".
type sessionModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Source   string `json:"source"`
	Effort   string `json:"effort,omitempty"`
}

func (s *Server) handleModels(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	var p struct {
		SessionID types.ID `json:"sessionId"`
		AgentID   types.ID `json:"agentId"`
		Provider  string   `json:"provider"`
		Model     string   `json:"model"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	st := s.app.Store
	switch req.Method {
	case protocol.MethodModelList:
		provs, err := st.ListProviders(ctx)
		if err != nil {
			return nil, err
		}
		out := []modelChoice{}
		seen := map[string]bool{}
		for _, pr := range provs {
			for _, m := range pr.Models {
				key := pr.Name + "\x00" + m
				if m == "" || seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, modelChoice{Provider: pr.Name, Model: m, Default: pr.Default})
			}
		}
		return core.MustJSON(out), nil

	case protocol.MethodSessionModel:
		sess, err := st.GetSession(ctx, p.SessionID)
		if err != nil {
			return nil, fmt.Errorf("session not found")
		}
		effort := st.GetSessionEffort(ctx, sess.ID)
		if m, err := st.GetSessionModel(ctx, sess.ID); err == nil {
			return core.MustJSON(sessionModel{Provider: m.Provider, Model: m.Model, Source: "chat", Effort: effort}), nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if r, err := persona.Resolve(ctx, st, sess.ID); err == nil && r.Model != "" {
			return core.MustJSON(sessionModel{Provider: r.Provider, Model: r.Model, Source: "profile", Effort: effort}), nil
		}
		aid := p.AgentID
		if aid == "" {
			aid = sess.AgentID
		}
		out := sessionModel{Source: "agent", Effort: effort}
		if ag, err := st.GetAgent(ctx, aid); err == nil {
			out.Provider, out.Model = ag.Provider, ag.Model
		}
		return core.MustJSON(out), nil

	case protocol.MethodSessionSetModel:
		if _, err := st.GetSession(ctx, p.SessionID); err != nil {
			return nil, fmt.Errorf("session not found")
		}
		if p.Model != "" {
			if err := s.knownModel(ctx, p.Provider, p.Model); err != nil {
				return nil, err
			}
		}
		if err := st.SetSessionModel(ctx, p.SessionID, types.ModelRef{Provider: p.Provider, Model: p.Model}); err != nil {
			return nil, err
		}
		return core.MustJSON(map[string]any{"ok": true}), nil
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}

// knownModel checks a pick against the configured providers, so a typo
// fails now instead of on the chat's next message.
func (s *Server) knownModel(ctx context.Context, provider, model string) error {
	provs, err := s.app.Store.ListProviders(ctx)
	if err != nil {
		return err
	}
	for _, pr := range provs {
		if provider != "" && pr.Name != provider {
			continue
		}
		for _, m := range pr.Models {
			if m == model {
				return nil
			}
		}
	}
	return fmt.Errorf("unknown model %q", model)
}
