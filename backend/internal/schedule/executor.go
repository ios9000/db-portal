package schedule

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
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
	id         int64
	instanceID int64
	instance   string
	operation  string
	cronSpec   string
	reason     *string
	createdBy  string
	confirm    string
}

func (s *Service) fireDue(ctx context.Context) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.instance_id, i.name, s.operation, s.cron_spec, s.reason, s.created_by, s.confirm
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
		if err := rows.Scan(&d.id, &d.instanceID, &d.instance, &d.operation, &d.cronSpec,
			&d.reason, &d.createdBy, &d.confirm); err != nil {
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
		// Bounded per fire (M2-gate finding 5): one stuck fire must not
		// starve the siblings behind it in this tick, nor block shutdown
		// forever.
		fctx, cancel := context.WithTimeout(ctx, s.FireTimeout)
		s.fire(fctx, d)
		cancel()
	}
}

// fire executes one due schedule: overlap check, then Start FIRST and stamp
// after (mini-ADR 3). The crash window between the two heals itself: the
// half-fired run gets swept failed at boot and the still-past next_fire_at
// retries the dump — never lost, never left running twice.
func (s *Service) fire(ctx context.Context, d dueSchedule) {
	// Re-validate against the live row: the due snapshot may predate a
	// concurrent disable or delete (M2-gate finding 2). A window of one
	// query remains; the stamp's enabled-guard covers the rest.
	var enabled bool
	err := s.pool.QueryRow(ctx,
		`SELECT enabled FROM schedule WHERE id = $1`, d.id).Scan(&enabled)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return // deleted since the snapshot
	case err != nil:
		s.log.Error("scheduler: fire re-check failed", "schedule", d.id, "err", err.Error())
		return // still due, retried next tick
	case !enabled:
		return // disabled since the snapshot — the human said stop
	}

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

	// Overlap is an INSTANCE property, not a schedule one (M2-gate finding
	// 3, amending mini-ADR 4): the hazard is two dumps loading one
	// database, whoever launched them — a sibling schedule or a human's
	// button press. Any live run on the instance skips this fire.
	var live bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM run
			WHERE instance_id = $1 AND state IN ('queued', 'running'))`,
		d.instanceID).Scan(&live); err != nil {
		// Leave next_fire_at untouched: still due, retried next tick.
		s.log.Error("scheduler: overlap check failed", "schedule", d.id, "err", err.Error())
		return
	}
	if live {
		// Skip, visibly (mini-ADR 4): overlapping dumps are a load
		// hazard and queueing builds an invisible backlog.
		s.log.Warn("scheduler: fire skipped, a run is already live on the instance",
			"schedule", d.id, "instance", d.instance)
		s.stamp(ctx, d.id, statusSkipped, nil, nil, next)
		return
	}

	var reason string
	if d.reason != nil {
		reason = *d.reason
	}
	// Confirm is the STORED creation-time ritual evidence (M2-gate finding
	// 1, amending mini-ADR 8) — never synthesized. An instance promoted to
	// prod after an unconfirmed creation fails the ritual in Start and
	// lands here as a visible 'error', not a silent prod dump.
	run, err := s.starter.Start(ctx, runs.StartRequest{
		Actor:     "schedule:" + d.createdBy,
		Instance:  d.instance,
		Operation: d.operation,
		Reason:    reason,
		Confirm:   d.confirm,
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
// It runs on its own bounded context: a fire that burned its whole
// FireTimeout still gets its outcome recorded (same posture as Start's
// stranded-job repair). The CASE guard keeps a concurrent disable's NULL
// intact (M2-gate finding 2): the fire outcome is recorded, the frozen
// clock is not overwritten.
func (s *Service) stamp(ctx context.Context, id int64, status string, runID *int64, firedAt *time.Time, next time.Time) {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(sctx, `
		UPDATE schedule SET last_fire_status = $2,
			last_run_id = COALESCE($3, last_run_id),
			last_fired_at = COALESCE($4, last_fired_at),
			next_fire_at = CASE WHEN enabled THEN $5::timestamptz ELSE NULL END,
			updated_at = now()
		WHERE id = $1`, id, status, runID, firedAt, next); err != nil {
		s.log.Error("scheduler: stamp failed", "schedule", id, "err", err.Error())
	}
}
