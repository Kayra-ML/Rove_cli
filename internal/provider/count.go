package provider

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Kayra-ML/rove/internal/types"
)

// Rove's own count of a model call, kept beside what the provider reports:
// the characters that actually went out (system prompt, history, tool
// definitions) and came back (text, tool calls), and the tokens they make.
// The characters are exact; the tokens are an estimate from them, since
// every model family cuts text its own way.
//
// The estimate follows how BPE tokenizers behave: plain English and code
// run about four characters a token, accented Latin letters (ş, ğ, é) are
// often split and cost more, and CJK characters cost about one each. Every
// message and tool also carries a few tokens of framing.

const (
	msgOverhead  = 4   // role and separators around each message
	toolOverhead = 8   // a tool's wrapper in the request
	imageTokens  = 800 // a typical picture at the size the app sends
)

// EstimateTokens is a text's token count by the rule above.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	var ascii, latin, wide float64
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf:
			ascii++
		case r <= 0x024F || unicode.In(r, unicode.Cyrillic, unicode.Greek):
			latin++
		default:
			wide++
		}
	}
	n := ascii/4 + latin/2 + wide
	if n < 1 {
		return 1
	}
	return int(n + 0.5)
}

// Measure is one call as Rove counted it.
type Measure struct {
	SentChars  int `json:"sentChars"`
	SentTokens int `json:"sentTokens"`
	RecvChars  int `json:"recvChars"`
	RecvTokens int `json:"recvTokens"`
	ImagesSent int `json:"imagesSent,omitempty"`
}

// MeasureRequest counts what a request sends.
func MeasureRequest(req ChatRequest) Measure {
	var m Measure
	for _, msg := range req.Messages {
		m.SentChars += utf8.RuneCountInString(msg.Content)
		m.SentTokens += EstimateTokens(msg.Content) + msgOverhead
		for _, c := range msg.ToolCalls {
			m.SentChars += utf8.RuneCountInString(c.Name) + utf8.RuneCountInString(c.ArgsJSON)
			m.SentTokens += EstimateTokens(c.Name) + EstimateTokens(c.ArgsJSON)
		}
		m.ImagesSent += len(msg.Images)
	}
	m.SentTokens += m.ImagesSent * imageTokens
	for _, t := range req.Tools {
		spec := t.Name + t.Description + string(t.Parameters)
		m.SentChars += utf8.RuneCountInString(spec)
		m.SentTokens += EstimateTokens(spec) + toolOverhead
	}
	return m
}

// AddReply counts what came back.
func (m *Measure) AddReply(text string, calls []types.ToolCall) {
	m.RecvChars += utf8.RuneCountInString(text)
	m.RecvTokens += EstimateTokens(text)
	for _, c := range calls {
		s := c.Name + c.ArgsJSON
		m.RecvChars += utf8.RuneCountInString(s)
		m.RecvTokens += EstimateTokens(s)
	}
	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return
	}
	m.RecvTokens += msgOverhead
}
