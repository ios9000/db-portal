// Package schedule implements the portal-owned scheduler (ADR-003,
// SPEC-022). One Service is both the schedule store behind the API and the
// executor: a DB-driven tick loop over next_fire_at — robfig/cron is the
// parser, never the runner (mini-ADR 1). Every fire goes through
// runs.Service.Start, the identical guardrail + audit path as the launch
// button, attributed `schedule:<owner>` (mini-ADR 9).
package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/ios9000/db-portal/backend/internal/catalog"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

var (
	// ErrNotFound is returned for an unknown schedule id.
	ErrNotFound = errors.New("schedule: not found")
	// ErrBadSpec wraps the parser's complaint about a cron spec; the
	// message is user-facing (it reaches the create drawer).
	ErrBadSpec = errors.New("invalid cron spec")
)

// Fire outcomes recorded on schedule.last_fire_status (CHECK-constrained).
const (
	statusFired   = "fired"
	statusSkipped = "skipped_overlap"
	statusError   = "error"
)

// Starter is the one door scheduled fires go through — satisfied by
// *runs.Service, faked in executor tests.
type Starter interface {
	Start(ctx context.Context, req runs.StartRequest) (runs.Run, error)
}

// Schedule is the read model served by the API. Env rides along from the
// instance join (the UI's env badge); NextFireAt is NULL iff disabled.
type Schedule struct {
	ID             int64      `json:"id"`
	Instance       string     `json:"instance"`
	Env            string     `json:"env"`
	Operation      string     `json:"operation"`
	CronSpec       string     `json:"cron_spec"`
	Reason         *string    `json:"reason"`
	Enabled        bool       `json:"enabled"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	NextFireAt     *time.Time `json:"next_fire_at"`
	LastFiredAt    *time.Time `json:"last_fired_at"`
	LastRunID      *int64     `json:"last_run_id"`
	LastFireStatus *string    `json:"last_fire_status"`
}

// CreateRequest is one schedule ask. CreatedBy is the session identity —
// explicit, like runs.StartRequest.Actor (SPEC-021 mini-ADR 4) — and
// becomes the owner every fire is attributed to. Confirm is the prod
// ritual, performed once at creation (mini-ADR 8).
type CreateRequest struct {
	Instance  string
	Operation string
	CronSpec  string
	Reason    string
	Confirm   string
	CreatedBy string
}

// Service is the schedule store + executor. Safe for concurrent use.
type Service struct {
	pool    *pgxpool.Pool
	starter Starter
	log     *slog.Logger

	// Tick is the executor's due-schedule poll cadence. Set before Run
	// (tests use ~1ms; the default suits minute-granularity cron).
	Tick time.Duration
	// Jitter bounds the random spread added to every computed fire time,
	// baked into the persisted next_fire_at (mini-ADR 6) so hundreds of
	// "daily at 02:00" dumps don't hit the estate in the same second.
	// Zero means exact cron times (tests).
	Jitter time.Duration
	// FireTimeout bounds one fire attempt (M2-gate finding 5): a hung DB
	// call or engine adapter must not wedge every other schedule and the
	// loop's own shutdown. Start's stranded-job repair already handles a
	// deadline landing mid-launch.
	FireTimeout time.Duration
}

func New(pool *pgxpool.Pool, starter Starter, log *slog.Logger) *Service {
	return &Service{
		pool: pool, starter: starter, log: log,
		Tick: 10 * time.Second, Jitter: time.Minute, FireTimeout: 30 * time.Second,
	}
}

const scheduleColumns = `
	SELECT s.id, i.name, i.env, s.operation, s.cron_spec, s.reason, s.enabled,
		s.created_by, s.created_at, s.next_fire_at, s.last_fired_at,
		s.last_run_id, s.last_fire_status
	FROM schedule s JOIN instance i ON i.id = s.instance_id`

func scanSchedule(row pgx.Row) (Schedule, error) {
	var sc Schedule
	err := row.Scan(&sc.ID, &sc.Instance, &sc.Env, &sc.Operation, &sc.CronSpec,
		&sc.Reason, &sc.Enabled, &sc.CreatedBy, &sc.CreatedAt, &sc.NextFireAt,
		&sc.LastFiredAt, &sc.LastRunID, &sc.LastFireStatus)
	return sc, err
}

// Create validates and stores a schedule, computing its first fire time.
// Reuses the runs error vocabulary where the check is the same one Start
// performs, so the handlers answer identically.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Schedule, error) {
	// The launchable gate, mirroring runs.Start (SPEC-031 mini-ADR 3): a
	// non-launchable chain step (verify/safety_dump/restore) is not a
	// schedulable operation. A schedule can never set Internal, so a
	// non-launchable op looks like an unknown one — same 400 as launch
	// (restore.md behavior 5). Existence alone (ByID finds chain steps too)
	// would write a permanently broken schedule that errors on every tick.
	op, ok := catalog.ByID(req.Operation)
	if !ok || !op.Launchable {
		return Schedule{}, fmt.Errorf("%w: %q", runs.ErrUnknownOperation, req.Operation)
	}
	spec, err := cron.ParseStandard(req.CronSpec)
	if err != nil {
		return Schedule{}, fmt.Errorf("%w: %v", ErrBadSpec, err)
	}

	var instanceID int64
	var env string
	err = s.pool.QueryRow(ctx, `SELECT id, env FROM instance WHERE name = $1`, req.Instance).
		Scan(&instanceID, &env)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Schedule{}, fmt.Errorf("%w: %q", runs.ErrUnknownInstance, req.Instance)
	case err != nil:
		return Schedule{}, fmt.Errorf("schedule: look up instance: %w", err)
	}
	// The prod ritual happens once, here, for every future fire: the
	// executor then confirms programmatically (mini-ADR 8).
	if env == "prod" && req.Confirm != req.Instance {
		return Schedule{}, runs.ErrProdUnconfirmed
	}

	// The typed confirm is PERSISTED, not just checked: the executor fires
	// with this exact string, so an instance promoted to prod after an
	// unconfirmed (non-prod) creation fails the ritual visibly at fire
	// time instead of auto-passing it (M2-gate finding 1).
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO schedule (instance_id, operation, cron_spec, reason, created_by, confirm, next_fire_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7)
		RETURNING id`,
		instanceID, req.Operation, req.CronSpec, req.Reason, req.CreatedBy, req.Confirm,
		s.nextFire(spec, time.Now())).Scan(&id)
	if err != nil {
		return Schedule{}, fmt.Errorf("schedule: insert: %w", err)
	}
	return s.get(ctx, id)
}

// List returns every schedule, newest first.
func (s *Service) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx, scheduleColumns+` ORDER BY s.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("schedule: list: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("schedule: scan: %w", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: list: %w", err)
	}
	return out, nil
}

func (s *Service) get(ctx context.Context, id int64) (Schedule, error) {
	sc, err := scanSchedule(s.pool.QueryRow(ctx, scheduleColumns+` WHERE s.id = $1`, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Schedule{}, ErrNotFound
	case err != nil:
		return Schedule{}, fmt.Errorf("schedule: get: %w", err)
	}
	return sc, nil
}

// SetEnabled toggles a schedule. Disabling freezes it (next_fire_at NULL);
// re-enabling computes the next fire from NOW — fires that would have
// happened while disabled are never made up (mini-ADR 5: misfire catch-up
// is for promises the portal broke, not promises a human revoked).
func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool) (Schedule, error) {
	var raw string
	var current bool
	err := s.pool.QueryRow(ctx,
		`SELECT cron_spec, enabled FROM schedule WHERE id = $1`, id).Scan(&raw, &current)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Schedule{}, ErrNotFound
	case err != nil:
		return Schedule{}, fmt.Errorf("schedule: get spec: %w", err)
	}
	// A redundant toggle is a true no-op (M2-gate finding 8): recomputing
	// would re-roll the jitter — and push a due-but-not-yet-fired schedule
	// a whole cron period out.
	if enabled == current {
		return s.get(ctx, id)
	}
	var next *time.Time
	if enabled {
		spec, err := cron.ParseStandard(raw)
		if err != nil {
			// Create validated the spec; only a hand-edited row gets here.
			return Schedule{}, fmt.Errorf("%w: %v", ErrBadSpec, err)
		}
		n := s.nextFire(spec, time.Now())
		next = &n
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE schedule SET enabled = $2, next_fire_at = $3, updated_at = now()
		WHERE id = $1`, id, enabled, next); err != nil {
		return Schedule{}, fmt.Errorf("schedule: toggle: %w", err)
	}
	return s.get(ctx, id)
}

// Delete removes a schedule. Its fired runs and their audit rows survive —
// the FK points from schedule to run, never back.
func (s *Service) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM schedule WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("schedule: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// nextFire is the next cron match after from, plus random jitter. Evaluated
// in server-local time (mini-ADR 7); persisted pre-jittered so the UI shows
// the true planned instant and the tick loop stays a dumb comparator.
func (s *Service) nextFire(spec cron.Schedule, from time.Time) time.Time {
	next := spec.Next(from)
	if s.Jitter > 0 {
		next = next.Add(rand.N(s.Jitter))
	}
	return next
}
