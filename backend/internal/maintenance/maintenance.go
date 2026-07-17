// Package maintenance implements the portal's periodic-sweep subsystem
// (SPEC-044): a tick-loop, mirroring the scheduler's lifecycle, that reaps
// expired sessions, enforces artifact retention, and observes the audit
// trail's age. Every pass is portal-DB work exercised under MockEngine
// (ADR-002); no pass ever mutates the append-only security ledgers.
package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service runs the maintenance sweeps. Safe for concurrent use; one instance
// per process, ticked by Run.
type Service struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	// Interval is the sweep cadence. Run sweeps once at boot then every
	// Interval; a non-positive value disables the loop (SPEC-044 mini-ADR 5).
	Interval time.Duration
	// ArtifactRetention is the max age of a `standard` registry artifact before
	// it is reaped. A non-positive value DISABLES the artifact pass — a zero age
	// would mean "reap everything", so it fails safe (mini-ADR 5). `safety`
	// artifacts are never reaped regardless.
	ArtifactRetention time.Duration
	// AuditRetention is the age past which the observational audit pass logs a
	// notice. It never deletes — the ledgers are append-only (mini-ADR 2).
	AuditRetention time.Duration
}

const (
	defaultInterval          = time.Hour
	defaultArtifactRetention = 90 * 24 * time.Hour  // 90d
	defaultAuditRetention    = 365 * 24 * time.Hour // 365d
)

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{
		pool: pool, log: log,
		Interval:          defaultInterval,
		ArtifactRetention: defaultArtifactRetention,
		AuditRetention:    defaultAuditRetention,
	}
}

// Report is one sweep pass's outcome — returned so tests assert counts without
// re-querying, and logged so an operator sees the maintenance heartbeat.
type Report struct {
	SessionsReaped  int64
	ArtifactsReaped int64
	// OrphanedObjects counts reaped `standard` artifacts that still carried a
	// non-NULL object-store location (real-engine only): their bytes are the
	// object store's lifecycle TTL to reclaim (mini-ADR 3), flagged so they are
	// never silently orphaned.
	OrphanedObjects int64
	// OldestAuditAge is the age of the oldest audit_event row, or 0 if the
	// trail is empty. Observational only (mini-ADR 2).
	OldestAuditAge time.Duration
}

// Run sweeps once at boot then every Interval until ctx is canceled. Main runs
// it as a goroutine beside the scheduler. A non-positive Interval disables the
// subsystem entirely (logged) — the process still serves.
func (s *Service) Run(ctx context.Context) {
	if s.Interval <= 0 {
		s.log.Warn("maintenance disabled (PORTAL_MAINTENANCE_INTERVAL is non-positive)")
		return
	}
	s.Sweep(ctx) // reap what accumulated during downtime, immediately
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}

// Sweep runs every maintenance pass once, best-effort: a failing pass logs and
// never skips the others (SPEC-044 mini-ADR 1). Returns the aggregate Report.
func (s *Service) Sweep(ctx context.Context) Report {
	var r Report
	r.SessionsReaped = s.reapExpiredSessions(ctx)
	r.ArtifactsReaped, r.OrphanedObjects = s.reapExpiredArtifacts(ctx)
	r.OldestAuditAge = s.observeAuditAge(ctx)
	return r
}

// reapExpiredSessions bulk-deletes sessions past their TTL (SPEC-044 mini-ADR
// 4). The predicate can never match a live session (expires_at > now()), so a
// live session is never reaped. Complements SPEC-020's lazy per-token expiry.
func (s *Service) reapExpiredSessions(ctx context.Context) int64 {
	tag, err := s.pool.Exec(ctx, `DELETE FROM session WHERE expires_at < now()`)
	if err != nil {
		s.log.Error("maintenance: session GC failed", "err", err.Error())
		return 0
	}
	if n := tag.RowsAffected(); n > 0 {
		s.log.Info("maintenance: expired sessions reaped", "count", n)
		return n
	}
	return 0
}

// reapExpiredArtifacts deletes `standard` registry artifacts older than
// ArtifactRetention, auditing each deletion, and NEVER touches `safety`
// (SPEC-044 mini-ADR 3). A non-positive retention disables the pass — a zero
// age would reap everything, so it fails safe (mini-ADR 5). Returns the number
// reaped and how many of those still carried an object-store location (bytes
// the store's lifecycle must reclaim — logged, never silently orphaned).
//
// The delete + audit ride ONE statement (modifying CTEs): the DELETE returns
// the reaped rows, an INSERT ... SELECT stamps an `artifact.reaped` audit_event
// linked to each artifact's origin run (its immutable run.submitted row carries
// instance_id/environment/playbook_tag), and the final SELECT surfaces the
// reaped locations. INSERT into the append-only audit_event is allowed — only
// UPDATE/DELETE are blocked (0003) — and readers filter on specific actions, so
// a new action never breaks them (the run.cancel_requested precedent).
func (s *Service) reapExpiredArtifacts(ctx context.Context) (reaped, orphanedObjects int64) {
	if s.ArtifactRetention <= 0 {
		s.log.Warn("maintenance: artifact retention disabled (PORTAL_ARTIFACT_RETENTION non-positive)")
		return 0, 0
	}
	rows, err := s.pool.Query(ctx, `
		WITH reaped AS (
			DELETE FROM artifact
			WHERE retention_class = 'standard'
			  AND created_at < now() - make_interval(secs => $1)
			RETURNING run_id, name, location
		),
		audited AS (
			INSERT INTO audit_event
				(actor, action, run_id, instance_id, environment, playbook_tag, params_digest)
			SELECT 'maintenance', 'artifact.reaped', ae.run_id, ae.instance_id,
				ae.environment, ae.playbook_tag, ae.params_digest
			FROM reaped rp
			JOIN audit_event ae ON ae.run_id = rp.run_id AND ae.action = 'run.submitted'
			RETURNING 1
		)
		SELECT name, location FROM reaped`,
		s.ArtifactRetention.Seconds())
	if err != nil {
		s.log.Error("maintenance: artifact retention sweep failed", "err", err.Error())
		return 0, 0
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var location *string
		if err := rows.Scan(&name, &location); err != nil {
			s.log.Error("maintenance: artifact reap scan failed", "err", err.Error())
			return reaped, orphanedObjects
		}
		reaped++
		if location != nil && *location != "" {
			orphanedObjects++
			// The registry row is gone but the bytes are the object store's
			// lifecycle TTL to reclaim (ADR-004; the portal never issues mc rm).
			// Surface it so an object is never silently orphaned.
			s.log.Warn("maintenance: reaped artifact still in object storage — ensure a bucket lifecycle rule reclaims it",
				"artifact", name, "location", *location)
		}
	}
	if err := rows.Err(); err != nil {
		s.log.Error("maintenance: artifact retention sweep failed", "err", err.Error())
		return reaped, orphanedObjects
	}
	if reaped > 0 {
		s.log.Info("maintenance: standard artifacts reaped past retention",
			"count", reaped, "retention", s.ArtifactRetention.String(), "still_in_object_store", orphanedObjects)
	}
	return reaped, orphanedObjects
}

// observeAuditAge reports the oldest audit_event row's age WITHOUT touching the
// table (SPEC-044 mini-ADR 2): the ledgers are append-only by trigger, so
// retention here is archive-then-purge, and cold-storage archival is post-MVP.
// Logs a notice when the trail exceeds AuditRetention so an operator knows when
// that archival becomes necessary. Returns the age (0 if empty).
func (s *Service) observeAuditAge(ctx context.Context) time.Duration {
	var oldest *time.Time
	err := s.pool.QueryRow(ctx, `SELECT min(ts) FROM audit_event`).Scan(&oldest)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0
	case err != nil:
		s.log.Error("maintenance: audit age probe failed", "err", err.Error())
		return 0
	}
	if oldest == nil {
		return 0 // empty trail — min() over no rows is NULL
	}
	age := time.Since(*oldest)
	if s.AuditRetention > 0 && age > s.AuditRetention {
		s.log.Info("maintenance: audit trail exceeds retention — cold-storage archival is post-MVP (retained in-DB)",
			"oldest_age", age.Round(time.Hour).String(), "retention", s.AuditRetention.String())
	}
	return age
}
