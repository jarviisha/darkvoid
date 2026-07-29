package app

import "sync/atomic"

// Codohue health states reported by GET /health.
const (
	// CodohueOff means CODOHUE_ENABLED is false — the feed uses local scoring by
	// design and nothing is wrong.
	CodohueOff = "off"
	// CodohueActive means the client is wired and Codohue answered its last probe.
	CodohueActive = "active"
	// CodohueDegraded means Codohue is configured but was not reachable. The API
	// serves normally: the feed falls back to local scoring and indexing calls
	// fail individually. Reported so this is visible to a monitor instead of
	// living in one startup log line.
	CodohueDegraded = "degraded"
)

// codohueStatus is the current Codohue state, published by startup and by the
// background monitor and read by the health handler on request goroutines.
//
// atomic rather than a plain field: the monitor writes it while handlers read it.
// It carries only the status, never the client — nothing is rewired at runtime,
// so no service field is ever written after the server starts.
type codohueStatus struct {
	state  atomic.Value // string
	reason atomic.Value // string, empty unless degraded
}

func newCodohueStatus() *codohueStatus {
	s := &codohueStatus{}
	s.set(CodohueOff, "")
	return s
}

func (s *codohueStatus) set(state, reason string) {
	s.state.Store(state)
	s.reason.Store(reason)
}

// get returns the current state and the reason it is degraded, if any.
func (s *codohueStatus) get() (state, reason string) {
	state, _ = s.state.Load().(string)
	reason, _ = s.reason.Load().(string)
	if state == "" {
		state = CodohueOff
	}
	return state, reason
}
