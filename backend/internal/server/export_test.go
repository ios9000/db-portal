package server

import "time"

// SetEndStateWait shrinks the SSE end-event terminal-state wait for tests
// that exercise the deadline path; returns a restore func for t.Cleanup.
func SetEndStateWait(d time.Duration) func() {
	old := endStateWait
	endStateWait = d
	return func() { endStateWait = old }
}
