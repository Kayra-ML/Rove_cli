package provider

import (
	"encoding/json"
	"testing"

	"github.com/Kayra-ML/rove/internal/types"
)

func TestEstimateTokens(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"hello world, this is a test", 7},    // 27 ASCII characters: about four a token
		{"şğüçöı", 3},                         // accented letters split: about two a token
		{"你好世界", 4},                           // one a CJK character
		{"func main() { fmt.Println(1) }", 8}, // code: 30 characters
	} {
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMeasureCountsEverythingSent(t *testing.T) {
	base := ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hello world, this is a test"}}}
	m := MeasureRequest(base)
	if m.SentChars != 27 || m.SentTokens != 7+msgOverhead {
		t.Fatalf("plain = %+v", m)
	}
	// tool definitions and pictures go out too, and are counted
	withTools := base
	withTools.Tools = []ToolSpec{{Name: "read_file", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object"}`)}}
	withTools.Messages = append(withTools.Messages, ChatMessage{Role: "user", Content: "bak", Images: []string{"data:image/png;base64,AAAA"}})
	mt := MeasureRequest(withTools)
	if mt.SentTokens <= m.SentTokens+imageTokens || mt.ImagesSent != 1 {
		t.Fatalf("with tools and an image = %+v", mt)
	}
	// the reply: its text and its tool calls
	mt.AddReply("done", []types.ToolCall{{Name: "read_file", ArgsJSON: `{"path":"a.go"}`}})
	if mt.RecvChars != 4+len("read_file")+len(`{"path":"a.go"}`) || mt.RecvTokens == 0 {
		t.Fatalf("reply = %+v", mt)
	}
}
