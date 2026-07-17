package runs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// defaultLockTTL sizes the instance-lock backstop (SPEC-042 mini-ADR 3). It is
// only a floor on how long a genuinely-leaked lock lingers before a reap — a
// lock whose holder run is still live is NEVER stolen regardless of TTL — so a
// generous value is safe. Primary release is finalize's own transaction and
// the boot sweep; this rarely fires.
const defaultLockTTL = 30 * time.Minute

func (s *Service) lockTTL() time.Duration {
	if s.LockTTL <= 0 {
		return defaultLockTTL
	}
	return s.LockTTL
}

func (s *Service) isProtected(instance string) bool {
	return s.Protected[strings.ToLower(instance)]
}

// acquireInstanceLock takes the per-instance lock for runID, or reports false
// if a live/unexpired holder has it (SPEC-042 mini-ADR 3). The steal predicate
// reclaims a lock ONLY when it is both expired AND its holder run is no longer
// live, so a still-running operation's lock is never taken from under it — the
// load-bearing safety property that makes the TTL harmless. Runs in the
// caller's transaction; concurrent acquirers serialize on the instance_id PK.
func acquireInstanceLock(ctx context.Context, tx pgx.Tx, instanceID, runID int64, actor string, ttl time.Duration) (bool, error) {
	var holder int64
	err := tx.QueryRow(ctx, `
		INSERT INTO instance_lock (instance_id, run_id, actor, expires_at)
		VALUES ($1, $2, $3, now() + make_interval(secs => $4))
		ON CONFLICT (instance_id) DO UPDATE
			SET run_id = EXCLUDED.run_id, actor = EXCLUDED.actor,
				acquired_at = now(), expires_at = EXCLUDED.expires_at
			WHERE instance_lock.expires_at < now()
			  AND NOT EXISTS (SELECT 1 FROM run r
				  WHERE r.id = instance_lock.run_id
					AND r.state IN ('queued', 'running'))
		RETURNING run_id`,
		instanceID, runID, actor, ttl.Seconds()).Scan(&holder)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // held by a live/unexpired holder — no row updated
	}
	if err != nil {
		return false, err
	}
	return holder == runID, nil
}

// Execer is the minimal write surface RecordGuardrailDenial needs — satisfied
// by both *pgxpool.Pool and pgx.Tx.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// RecordGuardrailDenial appends a guardrail.denied row to the auth_event
// security ledger (SPEC-042 mini-ADRs 5+6): a target refused by a structural
// guardrail — self-target or Patroni-restore — never becomes a run, so the
// denial lives here, not on the run-centric audit_event. kind labels which
// guardrail fired; instance is the refused target. Exported so chain.Create
// records the same shape.
func RecordGuardrailDenial(ctx context.Context, db Execer, actor, kind, instance string) error {
	_, err := db.Exec(ctx,
		`INSERT INTO auth_event (actor, action, detail) VALUES ($1, 'guardrail.denied', $2)`,
		actor, fmt.Sprintf("%s: %s", kind, instance))
	return err
}
