package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestPersonaSetAndCost(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	agents, _ := app.Agents.List(ctx)
	sess, _ := app.Sess.Create(ctx, "t", agents[0].ID, "")

	type view struct {
		Persona struct {
			Source      string   `json:"source"`
			CharacterID string   `json:"characterId"`
			Features    []string `json:"features"`
		} `json:"persona"`
		Cost struct {
			Total    int `json:"total"`
			ToolsOff int `json:"toolsOff"`
		} `json:"cost"`
	}
	call := func(method, params string) view {
		t.Helper()
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: json.RawMessage(params)})
		if !resp.OK {
			t.Fatalf("%s: %s", method, resp.Error)
		}
		var v view
		_ = json.Unmarshal(resp.Result, &v)
		return v
	}
	sid := string(sess.ID)
	base := call(protocol.MethodPersonaGet, `{"sessionId":"`+sid+`"}`)
	if base.Persona.Source != "none" || base.Cost.ToolsOff != 0 {
		t.Fatalf("base = %+v", base)
	}
	// fuzzy character name, as typed after /character
	v := call(protocol.MethodPersonaSet, `{"sessionId":"`+sid+`","characterId":"güvenlik"}`)
	if v.Persona.CharacterID != "security" || v.Cost.ToolsOff == 0 {
		t.Fatalf("character = %+v", v)
	}
	v = call(protocol.MethodPersonaSet, `{"sessionId":"`+sid+`","features":["read","bogus","concise"]}`)
	if len(v.Persona.Features) != 2 || v.Persona.CharacterID != "security" {
		t.Fatalf("features = %+v", v)
	}
	v = call(protocol.MethodPersonaClear, `{"sessionId":"`+sid+`"}`)
	if v.Persona.Source != "none" {
		t.Fatalf("clear = %+v", v)
	}
	if r := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodPersonaSet, Token: app.Token, Params: json.RawMessage(`{"sessionId":"` + sid + `","characterId":"nope-nope"}`)}); r.OK {
		t.Fatal("unknown character accepted")
	}
}
