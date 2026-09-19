package qualitygate

import (
	"context"
	"testing"

	"github.com/Kayra-ML/rove/internal/types"
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

func TestWorkDirEscape(t *testing.T) {
	r := New()
	dir := t.TempDir()
	res := r.Run(context.Background(), []types.QualityGate{
		{Name: "rel", Kind: types.GateCustom, Command: "pwd", WorkDir: "..", ExpectExitZero: true},
		{Name: "abs", Kind: types.GateCustom, Command: "pwd", WorkDir: "/tmp", ExpectExitZero: true},
		{Name: "ok", Kind: types.GateCustom, Command: "pwd", WorkDir: ".", ExpectExitZero: true},
	}, dir)
	if len(res) != 3 {
		t.Fatalf("len %d", len(res))
	}
	if res[0].Passed || res[0].Error == "" {
		t.Fatalf("relative escape should fail: %+v", res[0])
	}
	if res[1].Passed || res[1].Error == "" {
		t.Fatalf("abs escape should fail: %+v", res[1])
	}
	if !res[2].Passed {
		t.Fatalf("ok rel WorkDir: %+v", res[2])
	}
}
