package tool

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// MaxOutput caps what one tool call hands back to the model (bytes). Every
// result is stored in the chat and sent again on each later turn, so one
// 2 MB build log would otherwise cost more than the whole rest of the chat.
const MaxOutput = 30000

// Clip keeps the start and the end of s (where errors and summaries sit)
// and drops the middle, with a note saying how much went and how to get it.
// Short text passes through unchanged.
func Clip(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	headEnd := max * 6 / 10
	tailStart := len(s) - (max - headEnd)
	// whole runes, and whole lines when a line break is near
	for headEnd > 0 && !utf8.RuneStart(s[headEnd]) {
		headEnd--
	}
	for tailStart < len(s) && !utf8.RuneStart(s[tailStart]) {
		tailStart++
	}
	if i := strings.LastIndexByte(s[:headEnd], '\n'); i > headEnd/2 {
		headEnd = i + 1
	}
	if i := strings.IndexByte(s[tailStart:], '\n'); i >= 0 && i < (len(s)-tailStart)/2 {
		tailStart += i + 1
	}
	mid := s[headEnd:tailStart]
	return s[:headEnd] + cutNote(len(mid), strings.Count(mid, "\n")) + s[tailStart:]
}

func cutNote(bytes, lines int) string {
	return fmt.Sprintf("\n\n[rove: %d bytes (%d lines) cut from the middle of this output. If you need them, read a narrower range (read_file offset/limit) or filter the command (grep, head, tail).]\n\n", bytes, lines)
}

// capWriter collects a command's output in bounded memory: the first part
// and the last part, counting what falls between. A chatty build cannot
// fill the daemon's memory, and the model still sees how it began and ended.
type capWriter struct {
	mu        sync.Mutex
	headLimit int
	tailLimit int
	head      []byte
	tail      []byte
	dropped   int
	droppedNL int
}

func newCapWriter(max int) *capWriter {
	// room for the note, so String never needs a second cut
	room := max - 256
	return &capWriter{headLimit: room * 6 / 10, tailLimit: room * 4 / 10}
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if room := w.headLimit - len(w.head); room > 0 {
		k := min(room, len(p))
		w.head = append(w.head, p[:k]...)
		p = p[k:]
	}
	if len(p) == 0 {
		return n, nil
	}
	w.tail = append(w.tail, p...)
	// trim only once the tail is twice its size, so a stream of small
	// writes does not copy the tail on every one
	if len(w.tail) > 2*w.tailLimit {
		w.trim()
	}
	return n, nil
}

func (w *capWriter) trim() {
	cut := len(w.tail) - w.tailLimit
	if cut <= 0 {
		return
	}
	w.dropped += cut
	w.droppedNL += strings.Count(string(w.tail[:cut]), "\n")
	w.tail = append(w.tail[:0], w.tail[cut:]...)
}

func (w *capWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trim()
	if w.dropped == 0 {
		return string(w.head) + string(w.tail)
	}
	head, tail := w.head, w.tail
	// the cuts may have split a rune
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	for i := 0; i < utf8.UTFMax-1 && len(head) > 0; i++ {
		if r, size := utf8.DecodeLastRune(head); r != utf8.RuneError || size > 1 {
			break
		}
		head = head[:len(head)-1]
	}
	return string(head) + cutNote(w.dropped, w.droppedNL) + string(tail)
}
