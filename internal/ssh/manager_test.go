package ssh

import (
	"context"
	"testing"

	"github.com/aether-dev/aether/internal/types"
)

func TestOpenRequiresHost(t *testing.T) {
	m := New(nil)
	_, err := m.Open(context.Background(), types.SSHTarget{}, "", "")
	if err == nil {
		t.Fatal("expected host error")
	}
}
