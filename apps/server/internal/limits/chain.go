package limits

import (
	"context"
	"sync"
	"time"

	"peekaping/internal/modules/workspace/workspaceid"
)

// DefaultCacheFor is how long a Chain keeps what it read. With several api replicas, a workspace's
// limits that another replica changed take effect here within it (docs/adr/0005 section 4).
const DefaultCacheFor = 5 * time.Second

// Chain is the Source of an install: the workspace's own limits, then the install's defaults, then
// none. It reads them all at once and keeps them for a few seconds; a change it makes itself
// (SetOwn) takes effect at once in this process.
type Chain struct {
	store    Store
	defaults InstallDefaults
	cacheFor time.Duration
	now      func() time.Time

	mu       sync.Mutex
	cached   *Snapshot
	cachedAt time.Time
	// generation counts the changes this process made, so that a read that began before one is not
	// kept after it
	generation uint64
}

var _ Source = (*Chain)(nil)

// ChainOption changes how a Chain behaves, for a test that has a reason to
type ChainOption func(*Chain)

// WithCacheFor sets how long the Chain keeps what it read. 0 reads every time.
func WithCacheFor(d time.Duration) ChainOption { return func(c *Chain) { c.cacheFor = d } }

// WithClock sets the clock the cache goes by
func WithClock(now func() time.Time) ChainOption { return func(c *Chain) { c.now = now } }

func NewChain(store Store, defaults InstallDefaults, opts ...ChainOption) *Chain {
	c := &Chain{store: store, defaults: defaults, cacheFor: DefaultCacheFor, now: time.Now}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Chain) For(ctx context.Context, workspaceID workspaceid.ID) (Limits, Origin, error) {
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return Limits{}, OriginNone, err
	}
	l, origin := snapshot.For(workspaceID)
	return l, origin, nil
}

func (c *Chain) Snapshot(ctx context.Context) (Snapshot, error) {
	c.mu.Lock()
	if c.cached != nil && c.now().Sub(c.cachedAt) < c.cacheFor {
		snapshot := *c.cached
		c.mu.Unlock()
		return snapshot, nil
	}
	generation := c.generation
	c.mu.Unlock()

	// Read without the lock: a slow database holds up no other reader that the cache can serve
	snapshot, err := c.read(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	c.mu.Lock()
	if c.generation == generation {
		c.cached, c.cachedAt = &snapshot, c.now()
	}
	c.mu.Unlock()
	return snapshot, nil
}

func (c *Chain) read(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	if c.defaults != nil {
		defaults, err := c.defaults.Defaults(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.Defaults = defaults.normalized()
	}
	if c.store != nil {
		own, err := c.store.AllOwn(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.Own = own
	}
	return snapshot, nil
}

// SetOwn stores the workspace's own limits (see Store.SetOwn), and forgets what the Chain read, so
// that this process goes by the change from the next read on
func (c *Chain) SetOwn(ctx context.Context, workspaceID workspaceid.ID, own *Limits) error {
	err := c.store.SetOwn(ctx, workspaceID, own)
	c.Forget()
	return err
}

// Forget drops what the Chain read, so that the next read reads again
func (c *Chain) Forget() {
	c.mu.Lock()
	c.cached = nil
	c.generation++
	c.mu.Unlock()
}
