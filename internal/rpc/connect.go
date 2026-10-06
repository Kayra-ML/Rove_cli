package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kayra-ML/rove/internal/connect"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func (s *Server) handleConnect(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	a := s.app
	var p struct {
		ID     string `json:"id"`
		Step   string `json:"step"`
		Access string `json:"access"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	switch req.Method {
	case protocol.MethodConnectCatalog:
		return core.MustJSON(connect.Detect(ctx)), nil
	case protocol.MethodConnectTerminal:
		if p.Step != connect.StepInstall && p.Step != connect.StepLogin {
			return nil, fmt.Errorf("unknown step %q", p.Step)
		}
		if err := connect.OpenTerminal(p.ID, p.Step); err != nil {
			return nil, err
		}
		return core.MustJSON(map[string]any{"ok": true}), nil
	case protocol.MethodConnectAdd:
		sys, ok := connect.Find(p.ID)
		if !ok || sys.Kind != connect.KindAgent {
			return nil, fmt.Errorf("unknown agent system %q", p.ID)
		}
		if provider.FindBin(sys.Bin) == "" {
			return nil, fmt.Errorf("%s is not installed on this computer", sys.Name)
		}
		// one provider per system: adding again updates it
		list, err := a.Store.ListProviders(ctx)
		if err != nil {
			return nil, err
		}
		pv := types.Provider{Name: sys.Name, Kind: types.ProviderAgentCLI}
		for _, x := range list {
			if x.Kind == types.ProviderAgentCLI {
				if system, _, ok := provider.ParseAgentURL(x.BaseURL); ok && system == sys.ID {
					pv = x
				}
			}
		}
		pv.BaseURL = provider.AgentURL(sys.ID, p.Access)
		pv.Models = nil
		out, err := a.SaveProvider(ctx, pv)
		return core.MustJSON(out), err
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}
