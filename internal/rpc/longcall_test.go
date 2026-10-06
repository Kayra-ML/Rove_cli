package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

type slowModel struct{}

func (slowModel) Kind() types.ProviderKind { return types.ProviderFake }
func (slowModel) Name() string             { return "slow" }
func (slowModel) Complete(ctx context.Context, _ provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 1)
	go func() {
		defer close(ch)
		select {
		case <-time.After(300 * time.Millisecond):
			ch <- provider.ChatDelta{Done: true, Content: "bitti"}
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// A chat turn that takes longer than the server's write timeout still gets
// its answer to the caller: the work is not done for nothing.
func TestLongCallKeepsItsAnswer(t *testing.T) {
	old := httpWriteTimeout
	httpWriteTimeout = 100 * time.Millisecond
	defer func() { httpWriteTimeout = old }()

	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Router.Register("slow", slowModel{})
	ctx := context.Background()
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "s", Provider: "slow", Model: "m"})
	sess, _ := app.Sess.Create(ctx, "t", ag.ID, "")
	s := New(app)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.ServeHTTPOn(ln) }()
	body, _ := json.Marshal(protocol.Request{Method: protocol.MethodSessionSend, Token: app.Token, Params: json.RawMessage(`{"sessionId":"` + string(sess.ID) + `","content":"merhaba"}`)})
	res, err := http.Post("http://"+ln.Addr().String()+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the answer was lost: %v", err)
	}
	defer res.Body.Close()
	var out protocol.Response
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || !out.OK || !bytes.Contains(out.Result, []byte("bitti")) {
		t.Fatalf("answer = %+v %v", out, err)
	}
}
