package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// Generator produces unique IDs. Tests can inject a deterministic source.
type Generator struct {
	rand io.Reader
	seq  atomic.Uint64
}

func New() *Generator {
	return &Generator{rand: rand.Reader}
}

func NewWithReader(r io.Reader) *Generator {
	return &Generator{rand: r}
}

func (g *Generator) New() types.ID {
	var b [10]byte
	if _, err := io.ReadFull(g.rand, b[:]); err != nil {
		n := g.seq.Add(1)
		return types.ID(fmt.Sprintf("fb%x%x", time.Now().UnixNano(), n))
	}
	n := g.seq.Add(1)
	return types.ID(fmt.Sprintf("%s%04x", hex.EncodeToString(b[:]), n&0xffff))
}

var defaultGen = New()

func NewID() types.ID { return defaultGen.New() }
