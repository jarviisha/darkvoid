package codohue

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/jarviisha/codohue/sdk/go"
)

// fakeClock drives the cooldown without sleeping.
type fakeClock struct{ nanos atomic.Int64 }

func (c *fakeClock) now() time.Time          { return time.Unix(0, c.nanos.Load()) }
func (c *fakeClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

func newTestBreaker() (*breaker, *fakeClock) {
	clock := &fakeClock{}
	clock.nanos.Store(time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC).UnixNano())
	return &breaker{now: clock.now}, clock
}

var errTransport = errors.New("dial tcp: connection refused")

func TestBreaker_StaysClosedBelowTheThreshold(t *testing.T) {
	b, _ := newTestBreaker()

	for range breakerThreshold - 1 {
		b.observe(errTransport)
	}

	if !b.allow() {
		t.Errorf("circuit opened after %d failures, want it to tolerate up to %d",
			breakerThreshold-1, breakerThreshold)
	}
}

func TestBreaker_OpensAfterConsecutiveFailures(t *testing.T) {
	b, _ := newTestBreaker()

	for range breakerThreshold {
		b.observe(errTransport)
	}

	if b.allow() {
		t.Error("circuit should be open after the threshold — the point is to stop paying the timeout")
	}
}

// A success in between resets the count, or an intermittent failure would
// eventually open the circuit on a service that is mostly fine.
func TestBreaker_SuccessResetsTheFailureCount(t *testing.T) {
	b, _ := newTestBreaker()

	for range breakerThreshold - 1 {
		b.observe(errTransport)
	}
	b.observe(nil)
	for range breakerThreshold - 1 {
		b.observe(errTransport)
	}

	if !b.allow() {
		t.Error("a success between failures should have reset the count")
	}
}

func TestBreaker_LetsOneTrialThroughAfterTheCooldown(t *testing.T) {
	b, clock := newTestBreaker()
	for range breakerThreshold {
		b.observe(errTransport)
	}

	clock.advance(breakerCooldown - time.Second)
	if b.allow() {
		t.Fatal("circuit reopened before the cooldown elapsed")
	}

	clock.advance(2 * time.Second)
	if !b.allow() {
		t.Fatal("circuit should allow a trial once the cooldown elapsed")
	}
	// Exactly one trial: the winner re-stamps the open time so concurrent
	// callers do not stampede a service that may still be down.
	if b.allow() {
		t.Error("a second caller got through — the cooldown should admit one trial at a time")
	}
}

func TestBreaker_SuccessfulTrialClosesTheCircuit(t *testing.T) {
	b, clock := newTestBreaker()
	for range breakerThreshold {
		b.observe(errTransport)
	}
	clock.advance(breakerCooldown + time.Second)

	if !b.allow() {
		t.Fatal("expected a trial to be allowed")
	}
	b.observe(nil)

	// Two independent callers, both admitted: the circuit is closed for everyone,
	// not just for whoever won the trial.
	firstCaller := b.allow()
	secondCaller := b.allow()
	if !firstCaller || !secondCaller {
		t.Errorf("callers admitted = %v, %v; a successful trial should close the circuit for all of them",
			firstCaller, secondCaller)
	}
}

func TestBreaker_FailedTrialStartsANewCooldown(t *testing.T) {
	b, clock := newTestBreaker()
	for range breakerThreshold {
		b.observe(errTransport)
	}
	clock.advance(breakerCooldown + time.Second)

	if !b.allow() {
		t.Fatal("expected a trial to be allowed")
	}
	b.observe(errTransport)

	clock.advance(breakerCooldown - time.Second)
	if b.allow() {
		t.Error("the cooldown after a failed trial should run from the trial, not the original outage")
	}
}

// The distinction that keeps this from being a footgun: a request the service
// rejected is our bug. Opening the circuit on it would disable the integration
// for every caller because one call site sent something malformed.
func TestBreaker_ClientErrorsDoNotOpenTheCircuit(t *testing.T) {
	// 401 and 403 used to be in this list — see TestBreaker_AuthFailuresOpenTheCircuit.
	// 404 and 409 stay, but only with a request-shaped code: the same statuses
	// carrying a namespace code are condemnations of the whole namespace, which
	// TestBreaker_NamespaceFaultsOpenTheCircuit covers.
	for _, status := range []int{400, 404, 409, 422} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			b, _ := newTestBreaker()
			apiErr := &sdk.APIError{Status: status, Code: "bad_request", Message: "nope"}

			for range breakerThreshold * 2 {
				b.observe(apiErr)
			}

			if !b.allow() {
				t.Errorf("a %d answer means the service is up; the circuit must stay closed", status)
			}
		})
	}
}

// A rejected credential is not one caller's mistake — it fails every request
// identically until an operator changes something. Leaving the circuit closed
// meant /health had no live signal to override a stale "active" with, which is
// how a deleted namespace went unnoticed for six days.
func TestBreaker_AuthFailuresOpenTheCircuit(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			b, _ := newTestBreaker()
			apiErr := &sdk.APIError{Status: status, Code: "unauthorized", Message: "invalid or missing bearer token"}

			for range breakerThreshold {
				b.observe(apiErr)
			}

			if b.allow() {
				t.Errorf("a %d means every caller is rejected; the circuit must open", status)
			}
		})
	}
}

// The incident that motivated the auth cases was not actually a bad key — the
// namespace had been deleted when its Codohue instance was rebuilt, which answers
// 404 namespace_not_found. Leaving that closed means every feed page pays a round
// trip to be told the namespace is gone, for as long as it stays gone.
func TestBreaker_NamespaceFaultsOpenTheCircuit(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{http.StatusNotFound, "namespace_not_found"},
		{http.StatusConflict, "namespace_not_active"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			b, _ := newTestBreaker()
			apiErr := &sdk.APIError{Status: tc.status, Code: tc.code, Message: "namespace unusable"}

			for range breakerThreshold {
				b.observe(apiErr)
			}

			if b.allow() {
				t.Errorf("%d %s condemns the namespace for every caller; the circuit must open", tc.status, tc.code)
			}
		})
	}
}

// The other half of the same judgement: a 404 that names one object must not
// disable the integration. DeleteObject on an already-deleted post is routine.
func TestBreaker_ObjectNotFoundDoesNotOpenTheCircuit(t *testing.T) {
	b, _ := newTestBreaker()
	apiErr := &sdk.APIError{Status: http.StatusNotFound, Code: "object_not_found", Message: "no such object"}

	for range breakerThreshold * 2 {
		b.observe(apiErr)
	}

	if !b.allow() {
		t.Error("a missing object is one request's problem; the circuit must stay closed")
	}
}

func TestBreaker_ServerErrorsOpenTheCircuit(t *testing.T) {
	b, _ := newTestBreaker()
	apiErr := &sdk.APIError{Status: 503, Code: "unavailable", Message: "down"}

	for range breakerThreshold {
		b.observe(apiErr)
	}

	if b.allow() {
		t.Error("a 5xx is an availability failure and should open the circuit")
	}
}

// A cancelled caller says nothing about Codohue. Counting it would let a burst
// of client disconnects disable the integration.
func TestBreaker_CallerCancellationDoesNotOpenTheCircuit(t *testing.T) {
	b, _ := newTestBreaker()

	for range breakerThreshold * 2 {
		b.observe(context.Canceled)
	}

	if !b.allow() {
		t.Error("caller cancellation should not be counted as a Codohue failure")
	}
}

// A timeout is the failure this exists to make cheap: the service is there but
// too slow to be useful, and every caller pays for it until the circuit opens.
func TestBreaker_DeadlineExceededOpensTheCircuit(t *testing.T) {
	b, _ := newTestBreaker()

	for range breakerThreshold {
		b.observe(context.DeadlineExceeded)
	}

	if b.allow() {
		t.Error("a timeout is an availability failure and should open the circuit")
	}
}

func TestGuard_ShortCircuitsWithoutCallingThrough(t *testing.T) {
	c := &Client{breaker: &breaker{now: time.Now}}
	for range breakerThreshold {
		c.breaker.observe(errTransport)
	}

	called := false
	_, err := guard(c, func() (int, error) {
		called = true
		return 1, nil
	})

	if called {
		t.Error("the request went out while the circuit was open — no timeout was saved")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestGuard_PassesResultsThroughWhenClosed(t *testing.T) {
	c := &Client{breaker: newBreaker()}

	got, err := guard(c, func() (string, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("got = %q, want %q", got, "ok")
	}
}

// The breaker sits in the request path of every feed read, so it is written and
// read from many goroutines at once. Under -race this is what proves the
// lock-free implementation is actually safe.
func TestBreaker_ConcurrentUse(t *testing.T) {
	b := newBreaker()
	var wg sync.WaitGroup

	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 500 {
				if b.allow() {
					if (i+j)%3 == 0 {
						b.observe(errTransport)
					} else {
						b.observe(nil)
					}
				}
			}
		}()
	}
	wg.Wait()
}
