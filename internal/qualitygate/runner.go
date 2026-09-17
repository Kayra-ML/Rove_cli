package qualitygate

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"github.com/aether-dev/aether/internal/types"
)

type Runner struct{}

func New() *Runner { return &Runner{} }

func (r *Runner) Run(ctx context.Context, gates []types.QualityGate, workDir string) []types.GateResult {
	out := make([]types.GateResult, 0, len(gates))
	for _, g := range gates {
		out = append(out, r.runOne(ctx, g, workDir))
	}
	return out
}

func (r *Runner) runOne(ctx context.Context, g types.QualityGate, workDir string) types.GateResult {
	res := types.GateResult{Name: g.Name}
	if g.Command == "" {
		res.Error = "empty command"
		return res
	}
	dir := g.WorkDir
	if dir == "" {
		dir = workDir
	}
	timeout := time.Duration(g.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell(), shellFlag(), g.Command)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	res.Output = buf.String()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
			res.Error = err.Error()
		}
	}
	expectZero := g.Kind != types.GateCustom || g.ExpectExitZero
	if g.ExpectExitZero {
		expectZero = true
	}
	if expectZero {
		res.Passed = res.ExitCode == 0 && res.Error == ""
	} else {
		res.Passed = res.Error == ""
	}
	return res
}

func AllPassed(results []types.GateResult) bool {
	if len(results) == 0 {
		return true
	}
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}
