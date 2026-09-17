package qualitygate

import (
	"context"
	"testing"

	"github.com/aether-dev/aether/internal/types"
)

func TestRunPassAndFail(t *testing.T) {
	r := New()
	dir := t.TempDir()
	res := r.Run(context.Background(), []types.QualityGate{
		{Name: "ok", Kind: types.GateTest, Command: "exit 0", ExpectExitZero: true},
		{Name: "bad", Kind: types.GateLint, Command: "exit 2", ExpectExitZero: true},
	}, dir)
	if len(res) != 2 {
		t.Fatalf("len %d", len(res))
	}
	if !res[0].Passed {
		t.Fatalf("ok failed: %+v", res[0])
	}
	if res[1].Passed {
		t.Fatal("bad should fail")
	}
	if AllPassed(res) {
		t.Fatal("not all passed")
	}
}
