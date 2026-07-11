package schedule

import (
	"context"
	"errors"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ios9000/db-portal/backend/internal/runs"
)

// Run is the executor loop (SPEC-022 mini-ADR 1): every Tick, fire whatever
// is due. Misfire catch-up needs no special case — a next_fire_at that
// passed while the portal was down is simply due on the first tick, once,
// coalesced (mini-ADR 2). Blocks until ctx is canceled; main runs it as a
// goroutine beside the HTTP server.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.fireDue(ctx)
		}
	}
}

// dueSchedule is the executor's working view of one due row.
type dueSchedule struct {
	id        int64
	instance  string
	operation string
	cronSpec  string
	reason    *string
	createdBy string
	lastRunID *int64
}

func (s *Service) fireDue(ctx context.Context) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, i.name, s.operation, s.cron_spec, s.reason, s.created_by, s.last_run_id
		FROM schedule s JOIN instance i ON i.id = s.instance_id
		WHERE s.enabled AND s.next_fire_at <= now()
		ORDER BY s.next_fire_at`)
	if err != nil {
		s.log.Error("scheduler: due query failed", "err", err.Error())
		return
	}
	var due []dueSchedule
	for rows.Next() {
		var d dueSchedule
		if err := rows.Scan(&d.id, &d.instance, &d.operation, &d.cronSpec,
			&d.reason, &d.createdBy, &d.lastRunID); err != nil {
			rows.Close()
			s.log.Error("scheduler: due scan failed", "err", err.Error())
			return
		}
		due = append(due, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		s.log.Error("scheduler: due query failed", "err", err.Error())
		return
	}
	for _, d := range due {
		s.fire(ctx, d)
	}
}

// fire executes one due schedule: overlap check, then Start FIRST and stamp
// after (mini-ADR 3). The crash window between the two heals itself: the
// half-fired run gets swept failed at boot and the still-past next_fire_at
// retries the dump — never lost, never left running twice.
func (s *Service) fire(ctx context.Context, d dueSchedule) {
	spec, err := cron.ParseStandard(d.cronSpec)
	if err != nil {
		// Create validated the spec, so only a hand-edited row gets here —
		// and without a parsable spec there is no next fire to advance to.
		// Disable loudly instead of hot-looping (mini-ADR 10).
		s.log.Error("scheduler: unparsable cron spec, disabling schedule",
			"schedule", d.id, "err", err.Error())
		if _, uerr := s.pool.Exec(ctx, `
			UPDATE schedule SET enabled = false, next_fire_at = NULL,
				last_fire_status = 'error', updated_at = now()
			WHERE id = $1`, d.id); uerr != nil {
			s.log.Error("scheduler: disable failed", "schedule", d.id, "err", uerr.Error())
		}
		return
	}
	next := s.nextFire(spec, time.Now())

	if d.lastRunID != nil {
		var live bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM run
				WHERE id = $1 AND state IN ('queued', 'running'))`,
			*d.lastRunID).Scan(&live); err != nil {
			// Leave next_fire_at untouched: still due, retried next tick.
			s.log.Error("scheduler: overlap check failed", "schedule", d.id, "err", err.Error())
			return
		}
		if live {
			// Skip, visibly (mini-ADR 4): overlapping dumps are a load
			// hazard and queueing builds an invisible backlog.
			s.log.Warn("scheduler: fire skipped, previous run still live",
				"schedule", d.id, "run", *d.lastRunID)
			s.stamp(ctx, d.id, statusSkipped, nil, nil, next)
			return
		}
	}

	var reason string
	if d.reason != nil {
		reason = *d.reason
	}
	run, err := s.starter.Start(ctx, runs.StartRequest{
		Actor:     "schedule:" + d.createdBy,
		Instance:  d.instance,
		Operation: d.operation,
		Reason:    reason,
		Confirm:   d.instance, // the owner performed the ritual at creation (mini-ADR 8)
	})
	now := time.Now()
	switch {
	case err == nil:
		s.stamp(ctx, d.id, statusFired, &run.ID, &now, next)
	case errors.Is(err, runs.ErrEngine):
		// The run exists, finalized failed, and SPEC-014 already mailed the
		// DBA list — from the schedule's view that fire happened (mini-ADR 10).
		s.log.Error("scheduler: engine refused scheduled run",
			"schedule", d.id, "run", run.ID, "err", err.Error())
		s.stamp(ctx, d.id, statusFired, &run.ID, &now, next)
	default:
		s.log.Error("scheduler: fire failed", "schedule", d.id, "err", err.Error())
		s.stamp(ctx, d.id, statusError, nil, nil, next)
	}
}

// stamp records a fire attempt's outcome and advances next_fire_at — the
// one write that keeps the loop moving whatever the attempt did (mini-ADR
// 10: a broken schedule logs once per fire, not per tick, and never wedges).
func (s *Service) stamp(ctx context.Context, id int64, status string, runID *int64, firedAt *time.Time, next time.Time) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE schedule SET last_fire_status = $2,
			last_run_id = COALESCE($3, last_run_id),
			last_fired_at = COALESCE($4, last_fired_at),
			next_fire_at = $5, updated_at = now()
		WHERE id = $1`, id, status, runID, firedAt, next); err != nil {
		s.log.Error("scheduler: stamp failed", "schedule", id, "err", err.Error())
	}
}
