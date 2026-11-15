// Package bulkhead implements a semaphore-based bulkhead isolation pattern.
// It limits the number of concurrent in-flight requests per tenant to prevent
// a single tenant from exhausting all gateway goroutines and starving others.
package bulkhead

import (
	"context"
	"errors"
	"sync"
)

// ErrBulkheadFull is returned when the bulkhead is at capacity.
var ErrBulkheadFull = errors.New("bulkhead full: too many concurrent requests")

// Bulkhead enforces a maximum concurrency limit per logical partition (tenant).
type Bulkhead struct {
	mu      sync.Mutex
	sem     chan struct{}
	max     int
	current int
}

// New creates a new Bulkhead that allows at most limit concurrent operations.
func New(limit int) *Bulkhead {
	return &Bulkhead{
		sem: make(chan struct{}, limit),
		max: limit,
	}
}

// Acquire attempts to acquire a slot in the bulkhead.
// It returns a release function if successful, or ErrBulkheadFull immediately
// if the bulkhead is at capacity (non-blocking).
func (b *Bulkhead) Acquire(_ context.Context) (release func(), err error) {
	select {
	case b.sem <- struct{}{}:
		b.mu.Lock()
		b.current++
		b.mu.Unlock()
		// The release func is idempotent: a second call (e.g. a deferred release
		// plus an explicit one) must not free another caller's slot or block.
		var once sync.Once
		return func() {
			once.Do(func() {
				<-b.sem
				b.mu.Lock()
				b.current--
				b.mu.Unlock()
			})
		}, nil
	default:
		return nil, ErrBulkheadFull
	}
}

// Current returns the number of currently occupied slots.
func (b *Bulkhead) Current() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.current
}

// Max returns the configured maximum concurrency.
func (b *Bulkhead) Max() int {
	return b.max
}
