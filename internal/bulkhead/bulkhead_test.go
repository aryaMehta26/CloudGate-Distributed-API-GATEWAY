package bulkhead

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAcquireRejectsWhenFull(t *testing.T) {
	b := New(2)
	r1, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Acquire(context.Background()); !errors.Is(err, ErrBulkheadFull) {
		t.Fatalf("third acquire: %v", err)
	}
	r1()
	if b.Current() != 1 {
		t.Fatalf("current = %d", b.Current())
	}
	if _, err = b.Acquire(context.Background()); err != nil {
		t.Fatal("slot not freed by release")
	}
	r2()
}

func TestReleaseIsIdempotent(t *testing.T) {
	b := New(2)
	r1, _ := b.Acquire(context.Background())
	r2, _ := b.Acquire(context.Background())
	r1()
	r1() // must not free r2's slot
	if b.Current() != 1 {
		t.Fatalf("double release changed count to %d", b.Current())
	}
	r2()
	r2() // must not block on an empty semaphore
	if b.Current() != 0 {
		t.Fatalf("current = %d", b.Current())
	}
}

func TestNeverExceedsMaxUnderContention(t *testing.T) {
	const max = 4
	b := New(max)
	var inFlight, peak atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := b.Acquire(context.Background())
			if err != nil {
				return
			}
			defer release()
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			inFlight.Add(-1)
		}()
	}
	wg.Wait()
	if peak.Load() > max || b.Current() != 0 {
		t.Fatalf("peak %d (max %d), current %d", peak.Load(), max, b.Current())
	}
}
