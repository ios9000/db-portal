package runs

import "context"

// Test-only exports (WU-016): the black-box test package needs to drive
// two failure paths deterministically that no public API reaches.

// SetFailRecordJobID makes every subsequent recordJobID call fail with err,
// exercising the stranded-job repair in Start.
func (s *Service) SetFailRecordJobID(err error) { s.failRecordJobID = err }

// Finalize exposes finalize so tests can play the losing side of a
// double-finalization race (watcher vs cancel vs sweep).
func (s *Service) Finalize(ctx context.Context, runID int64, state, errMsg string) error {
	return s.finalize(ctx, runID, state, errMsg, nil, nil, nil)
}
