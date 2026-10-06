// Package diffx makes unified line diffs for reviewing a turn's changes.
package diffx

import (
	"fmt"
	"strings"
)

// MaxLines caps each side; bigger files get a summary instead of a diff.
const MaxLines = 4000

// Stats counts added and removed lines.
type Stats struct{ Added, Removed int }

// Unified returns a unified diff (3 lines of context) from a to b, named
// path, and its line counts. Identical inputs give "".
func Unified(path, a, b string) (string, Stats) {
	if a == b {
		return "", Stats{}
	}
	x, y := lines(a), lines(b)
	if len(x) > MaxLines || len(y) > MaxLines {
		return fmt.Sprintf("--- a/%s\n+++ b/%s\n(file too large to show: %d → %d lines)\n", path, path, len(x), len(y)), Stats{Added: len(y), Removed: len(x)}
	}
	ops := diff(x, y)
	var st Stats
	for _, o := range ops {
		switch o.kind {
		case '+':
			st.Added++
		case '-':
			st.Removed++
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", path, path)
	const ctx = 3
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// a hunk: from ctx lines before this change to ctx lines after the
		// last change within 2*ctx of it
		start := i - ctx
		if start < 0 {
			start = 0
		}
		end := i
		for j := i; j < len(ops); j++ {
			if ops[j].kind != ' ' {
				end = j
			} else if j-end > 2*ctx {
				break
			}
		}
		stop := end + ctx + 1
		if stop > len(ops) {
			stop = len(ops)
		}
		aStart, bStart, aLen, bLen := ops[start].a, ops[start].b, 0, 0
		for _, o := range ops[start:stop] {
			if o.kind != '+' {
				aLen++
			}
			if o.kind != '-' {
				bLen++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart+1, aLen, bStart+1, bLen)
		for _, o := range ops[start:stop] {
			out.WriteByte(o.kind)
			out.WriteString(o.text)
			out.WriteByte('\n')
		}
		i = stop
	}
	return out.String(), st
}

type op struct {
	kind byte // ' ', '-', '+'
	text string
	a, b int // line index in a and b where this op sits
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// diff is a longest-common-subsequence line diff; common prefix and suffix
// are taken off first so typical edits stay cheap.
func diff(x, y []string) []op {
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	mx, my := x[pre:len(x)-suf], y[pre:len(y)-suf]
	n, m := len(mx), len(my)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if mx[i] == my[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []op
	for k := 0; k < pre; k++ {
		ops = append(ops, op{' ', x[k], k, k})
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && mx[i] == my[j]:
			ops = append(ops, op{' ', mx[i], pre + i, pre + j})
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{'+', my[j], pre + i, pre + j})
			j++
		default:
			ops = append(ops, op{'-', mx[i], pre + i, pre + j})
			i++
		}
	}
	for k := 0; k < suf; k++ {
		ops = append(ops, op{' ', x[len(x)-suf+k], len(x) - suf + k, len(y) - suf + k})
	}
	return ops
}
