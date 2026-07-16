// Package circuitbreaker implements the circuit breaker pattern for upstream protection.
// The FSM has three states:
//   - Closed:   Normal operation; failures are counted.
//   - Open:     Upstream is failing; requests are rejected immediately without forwarding.
//   - Half-Open: After OpenTimeout, one probe request is allowed to test recovery.
package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is in the Open state.
var ErrCircuitOpen = errors.New("circuit breaker open")

// State represents the current state of the circuit breaker.
type State int

const (
	StateClosed   State = iota
	StateOpen
	StateHalfOpen
)

// String returns the human-readable name of the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker guards upstream calls and implements the FSM.
type CircuitBreaker struct {
	mu sync.Mutex

	state State

	// Closed state tracking
	failureCount     int
	failureThreshold int

	// Open state tracking
	openAt      time.Time
	openTimeout time.Duration

	// Half-Open state tracking
	successCount     int
	successThreshold int

	// Optional state-change callback for metrics
	onStateChange func(name string, from, to State)

	name string
}

// New creates a new CircuitBreaker with the provided settings.
func New(name string, failureThreshold int, openTimeout time.Duration, successThreshold int) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		state:            StateClosed,
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
		successThreshold: successThreshold,
	}
}

// WithStateChangeCallback registers a callback for state transitions.
func (cb *CircuitBreaker) WithStateChangeCallback(fn func(name string, from, to State)) *CircuitBreaker {
	cb.onStateChange = fn
	return cb
}

// Allow returns nil if the request can proceed, or ErrCircuitOpen if blocked.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return nil

	case StateOpen:
		if time.Since(cb.openAt) >= cb.openTimeout {
			cb.transition(StateHalfOpen)
			return nil // allow probe
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		return nil // allow single probe
	}

	return nil
}

// RecordSuccess records a successful upstream call.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failureCount = 0

	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.transition(StateClosed)
		}
	}
}

// RecordFailure records a failed upstream call.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.transition(StateOpen)
		}

	case StateHalfOpen:
		cb.transition(StateOpen)
	}
}

// State returns the current state (safe for concurrent reads).
func (cb *CircuitBreaker) CurrentState() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// transition moves the CB to a new state and fires the callback.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) transition(to State) {
	from := cb.state
	cb.state = to

	// Reset counters
	switch to {
	case StateClosed:
		cb.failureCount = 0
		cb.successCount = 0
	case StateOpen:
		cb.openAt = time.Now()
		cb.successCount = 0
	case StateHalfOpen:
		cb.successCount = 0
	}

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, from, to)
	}
}
