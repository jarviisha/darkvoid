package codohue

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	sdk "github.com/jarviisha/codohue/sdk/go"
)

// ErrUnavailable is returned instead of making a request while the circuit is
// open. Callers already treat any Codohue error as "skip it and fall back", so
// this needs no special handling — it just arrives immediately.
var ErrUnavailable = errors.New("codohue: unavailable, circuit open")

const (
	// breakerThreshold is how many consecutive availability failures open the
	// circuit. Above one so a single blip does not disable anything; low enough
	// that a real outage costs only a few slow requests.
	breakerThreshold = 3
	// breakerCooldown is how long the circuit stays open before letting one
	// trial request through. Deliberately shorter than the app's health probe
	// interval, so a probe always finds the circuit willing to be tested.
	breakerCooldown = 30 * time.Second
)

// breaker is a circuit breaker over Codohue's HTTP surface.
//
// Every call site already degrades gracefully — the feed falls back to local
// scoring, ingestion logs and moves on — but "gracefully" still meant paying the
// per-call timeout first. During an outage that is 2-3 seconds added to every
// feed request, for a result that is thrown away. This makes the second and
// subsequent failures free.
//
// Lock-free because it sits in the request path of every feed read: two atomics
// and no allocation, versus a mutex every request would contend on.
type breaker struct {
	failures atomic.Int32
	// openedAt is the unix-nano instant the circuit opened, 0 when closed.
	openedAt atomic.Int64
	// now is injectable so the cooldown can be tested without sleeping. Set once
	// at construction and never written again.
	now func() time.Time
}

func newBreaker() *breaker {
	return &breaker{now: time.Now}
}

// isOpen reports whether the circuit is currently open, without claiming a
// trial the way allow() does. Read-only, for health reporting.
func (b *breaker) isOpen() bool {
	return b.openedAt.Load() != 0
}

// allow reports whether a request may go out.
func (b *breaker) allow() bool {
	opened := b.openedAt.Load()
	if opened == 0 {
		return true
	}
	if b.now().Sub(time.Unix(0, opened)) < breakerCooldown {
		return false
	}
	// Cooldown elapsed. Re-stamp the open time to claim the trial: exactly one
	// caller wins the swap and probes, everyone else still sees an open circuit
	// rather than all of them stampeding a service that may still be down.
	return b.openedAt.CompareAndSwap(opened, b.now().UnixNano())
}

// observe records the outcome of a call that was allowed through.
func (b *breaker) observe(err error) {
	if !tripsBreaker(err) {
		// Includes 4xx: the service answered, so it is available. Whatever is
		// wrong with the request will not be fixed by cutting off every caller.
		b.failures.Store(0)
		b.openedAt.Store(0)
		return
	}
	if b.failures.Add(1) >= breakerThreshold {
		// Only from closed: a failed trial leaves the timestamp allow() stamped,
		// which starts the next cooldown from the trial rather than the original
		// outage.
		b.openedAt.CompareAndSwap(0, b.now().UnixNano())
	}
}

// tripsBreaker reports whether err means Codohue is unavailable, as opposed to
// the request being wrong or the caller giving up.
func tripsBreaker(err error) bool {
	if err == nil {
		return false
	}
	// The caller hung up; says nothing about Codohue.
	if errors.Is(err, context.Canceled) {
		return false
	}
	var apiErr *sdk.APIError
	if errors.As(err, &apiErr) {
		// 4xx is our bug — a malformed request would otherwise open the circuit
		// and disable the integration for everyone.
		return apiErr.Status >= 500
	}
	// Transport failure: DNS, connection refused, TLS, deadline exceeded.
	return true
}

// guard runs fn unless the circuit is open, and records the outcome.
func guard[T any](c *Client, fn func() (T, error)) (T, error) {
	if !c.breaker.allow() {
		var zero T
		return zero, ErrUnavailable
	}
	v, err := fn()
	c.breaker.observe(err)
	return v, err
}

// guardErr is guard for calls that return only an error.
func guardErr(c *Client, fn func() error) error {
	_, err := guard(c, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}
