package lease

import (
	"context"
	"time"

	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
)

// Coordinator prevents two agents from editing the same file at once.
type Coordinator struct {
	store *store.Store
	ttl   time.Duration
}

func New(s *store.Store, ttl time.Duration) *Coordinator {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &Coordinator{store: s, ttl: ttl}
}

func (c *Coordinator) Acquire(ctx context.Context, path, holder string, card types.ID) error {
	_ = c.store.SweepExpiredLeases(ctx, time.Now().UTC())
	return c.store.AcquireLease(ctx, path, holder, card, time.Now().UTC().Add(c.ttl))
}

func (c *Coordinator) Release(ctx context.Context, path, holder string) error {
	return c.store.ReleaseLease(ctx, path, holder)
}

func (c *Coordinator) AcquireMany(ctx context.Context, paths []string, holder string, card types.ID) error {
	acquired := make([]string, 0, len(paths))
	for _, p := range paths {
		if err := c.Acquire(ctx, p, holder, card); err != nil {
			for _, a := range acquired {
				_ = c.Release(ctx, a, holder)
			}
			return err
		}
		acquired = append(acquired, p)
	}
	return nil
}
