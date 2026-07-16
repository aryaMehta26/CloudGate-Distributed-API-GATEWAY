package circuitbreaker

import (
	"testing"
	"time"
)

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	cb := New("test", 3, 100*time.Millisecond, 2)

	// 3 failures should trip the breaker
	for i := 0; i < 3; i++ {
		if err := cb.Allow(); err != nil {
			t.Fatalf("should be allowed in closed state, got %v", err)
		}
		cb.RecordFailure()
	}

	if cb.CurrentState() != StateOpen {
		t.Fatalf("expected Open state, got %s", cb.CurrentState())
	}

	// Should be rejected in open state
	if err := cb.Allow(); err != ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	cb := New("test", 1, 50*time.Millisecond, 1)

	cb.Allow()           //nolint:errcheck
	cb.RecordFailure()   // trips to open

	if cb.CurrentState() != StateOpen {
		t.Fatal("expected Open state")
	}

	// Before timeout: still open
	if err := cb.Allow(); err != ErrCircuitOpen {
		t.Fatal("should be open before timeout")
	}

	time.Sleep(60 * time.Millisecond)

	// After timeout: transitions to half-open, probe allowed
	if err := cb.Allow(); err != nil {
		t.Fatalf("probe should be allowed after timeout: %v", err)
	}

	if cb.CurrentState() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %s", cb.CurrentState())
	}
}

func TestCircuitBreaker_HalfOpenToClose(t *testing.T) {
	cb := New("test", 1, 10*time.Millisecond, 2)

	cb.Allow()         //nolint:errcheck
	cb.RecordFailure() // open

	time.Sleep(15 * time.Millisecond)
	cb.Allow() //nolint:errcheck // half-open probe

	cb.RecordSuccess()
	if cb.CurrentState() != StateHalfOpen {
		t.Fatal("should still be half-open after 1 success (threshold=2)")
	}

	cb.Allow() //nolint:errcheck
	cb.RecordSuccess()
	if cb.CurrentState() != StateClosed {
		t.Fatalf("expected Closed after 2 successes, got %s", cb.CurrentState())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := New("test", 1, 10*time.Millisecond, 1)

	cb.Allow()         //nolint:errcheck
	cb.RecordFailure() // open

	time.Sleep(15 * time.Millisecond)
	cb.Allow() //nolint:errcheck // half-open probe
	cb.RecordFailure()

	if cb.CurrentState() != StateOpen {
		t.Fatalf("failure in half-open should reopen: got %s", cb.CurrentState())
	}
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	var transitions []string
	cb := New("test-cb", 1, 10*time.Millisecond, 1)
	cb.WithStateChangeCallback(func(name string, from, to State) {
		transitions = append(transitions, from.String()+"->"+to.String())
	})

	cb.Allow()         //nolint:errcheck
	cb.RecordFailure() // closed -> open

	time.Sleep(15 * time.Millisecond)
	cb.Allow()         //nolint:errcheck
	cb.RecordSuccess() // open -> half-open -> closed (2 transitions)

	if len(transitions) < 2 {
		t.Fatalf("expected at least 2 transitions, got %v", transitions)
	}
}
