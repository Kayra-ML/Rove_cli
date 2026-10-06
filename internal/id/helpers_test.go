package id

import "io"

// NewWithReader makes ids from a fixed source, for tests.
func NewWithReader(r io.Reader) *Generator {
	return &Generator{rand: r}
}
