package runs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ios9000/db-portal/backend/internal/engine"
)

// jobRef resolves a run id to what the engine needs: its job id, adapter
// class and current state. jobID is nil when the engine refused the run
// before a job existed.
func (s *Service) jobRef(ctx context.Context, id int64) (jobID *string, class engine.EnvClass, state string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT job_id, engine_class, state FROM run WHERE id = $1`, id).
		Scan(&jobID, &class, &state)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, "", "", ErrNotFound
	case err != nil:
		return nil, "", "", fmt.Errorf("runs: look up run %d: %w", id, err)
	}
	return jobID, class, state, nil
}

// StreamLogs bridges a run to its engine job's log stream (WU-005 contract:
// full replay, then follow; the channel closes when the job is terminal or
// ctx is cancelled). ErrNoLogs when no job was started or the engine lost it.
func (s *Service) StreamLogs(ctx context.Context, id int64) (<-chan engine.LogLine, error) {
	jobID, class, _, err := s.jobRef(ctx, id)
	if err != nil {
		return nil, err
	}
	if jobID == nil {
		return nil, ErrNoLogs
	}
	adapter, err := s.registry.For(class)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoLogs, err.Error())
	}
	ch, err := adapter.StreamLogs(ctx, engine.JobID(*jobID))
	if errors.Is(err, engine.ErrUnknownJob) {
		return nil, fmt.Errorf("%w: %s", ErrNoLogs, err.Error())
	}
	if err != nil {
		return nil, fmt.Errorf("runs: stream logs for run %d: %w", id, err)
	}
	return ch, nil
}

// Cancel asks the engine to stop a run's job. The transition to `canceled`
// is asynchronous: the run's watcher observes it and finalizes as usual
// (SPEC-013 mini-ADR 4 — cancel adds no new finalization path).
func (s *Service) Cancel(ctx context.Context, id int64) error {
	jobID, class, state, err := s.jobRef(ctx, id)
	if err != nil {
		return err
	}
	if engine.JobState(state).Terminal() || jobID == nil {
		return ErrNotCancelable
	}
	adapter, err := s.registry.For(class)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotCancelable, err.Error())
	}
	err = adapter.Cancel(ctx, engine.JobID(*jobID))
	if errors.Is(err, engine.ErrUnknownJob) {
		return fmt.Errorf("%w: %s", ErrNotCancelable, err.Error())
	}
	if err != nil {
		return fmt.Errorf("%w: %s", ErrEngine, err.Error())
	}
	return nil
}
