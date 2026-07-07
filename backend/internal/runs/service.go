// Package runs implements the SPEC-012 hero flow: launching an operation
// as an engine job with an append-only audit trail, watching it to a
// terminal state, and serving run state to the API. This is the only place
// portal code talks to the engine Registry.
package runs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ios9000/db-portal/backend/internal/catalog"
	"github.com/ios9000/db-portal/backend/internal/engine"
)

var (
	// ErrUnknownInstance is returned by Start for an instance name not in
	// the inventory.
	ErrUnknownInstance = errors.New("runs: unknown instance")
	// ErrUnknownOperation is returned by Start for an operation id not in
	// the catalog.
	ErrUnknownOperation = errors.New("runs: unknown operation")
	// ErrEngine wraps engine failures at submit time: the run exists and is
	// already finalized failed when this is returned.
	ErrEngine = errors.New("runs: engine refused the job")
	// ErrNotFound is returned by Get for an unknown run id.
	ErrNotFound = errors.New("runs: run not found")
)

// actor is the audit identity placeholder until authn/authz land
// (WU-020/021, SPEC-012 mini-ADR 6).
const actor = "local-dev"

// Service launches and tracks runs. Safe for concurrent use.
type Service struct {
	pool     *pgxpool.Pool
	registry *engine.Registry
	log      *slog.Logger

	// PollInterval is the watcher's engine-status poll cadence. Set before
	// first use (tests use ~1ms; default suits the dev mock).
	PollInterval time.Duration

	wg sync.WaitGroup
}

func NewService(pool *pgxpool.Pool, registry *engine.Registry, log *slog.Logger) *Service {
	return &Service{pool: pool, registry: registry, log: log, PollInterval: 500 * time.Millisecond}
}

// Wait blocks until every watcher goroutine has finished. Test helper;
// production never calls it (watchers die with the process, the orphan
// sweep repairs on next boot).
func (s *Service) Wait() { s.wg.Wait() }

// Start launches operationID on the named instance: run row + audit
// `run.submitted` first (one tx), then the engine job via the Registry.
// Engine refusal finalizes the run failed and returns ErrEngine — the audit
// trail records the attempt either way. engineParams is for tests and
// future parameterized ops; the API passes nil (MVP accepts no client params).
func (s *Service) Start(ctx context.Context, instanceName, operationID, reason string, engineParams map[string]string) (Run, error) {
	op, ok := catalog.ByID(operationID)
	if !ok {
		return Run{}, fmt.Errorf("%w: %q", ErrUnknownOperation, operationID)
	}

	var instanceID int64
	var env string
	err := s.pool.QueryRow(ctx, `SELECT id, env FROM instance WHERE name = $1`, instanceName).
		Scan(&instanceID, &env)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Run{}, fmt.Errorf("%w: %q", ErrUnknownInstance, instanceName)
	case err != nil:
		return Run{}, fmt.Errorf("runs: look up instance: %w", err)
	}

	class, err := engine.ClassForEnv(env) // fails closed on garbage
	if err != nil {
		return Run{}, fmt.Errorf("runs: %w", err)
	}

	params := map[string]string{"instance": instanceName}
	for k, v := range engineParams {
		params[k] = v
	}
	digest := paramsDigest(params)

	// Record intent before touching the engine: run + audit `run.submitted`
	// commit atomically, so every launch attempt leaves a trail.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Run{}, fmt.Errorf("runs: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var runID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state, reason)
		VALUES ($1, $2, $3, $4, $5, 'queued', NULLIF($6, ''))
		RETURNING id`,
		instanceID, op.ID, env, string(class), op.PlaybookTag, reason).Scan(&runID)
	if err != nil {
		return Run{}, fmt.Errorf("runs: insert run: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest)
		VALUES ($1, 'run.submitted', $2, $3, $4, $5, $6)`,
		actor, runID, instanceID, env, op.PlaybookTag, digest); err != nil {
		return Run{}, fmt.Errorf("runs: audit submit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, fmt.Errorf("runs: commit: %w", err)
	}

	adapter, err := s.registry.For(class)
	if err == nil {
		var jobID engine.JobID
		if jobID, err = adapter.StartJob(ctx, op.Template, params); err == nil {
			if _, uerr := s.pool.Exec(ctx, `UPDATE run SET job_id = $2, updated_at = now() WHERE id = $1`,
				runID, string(jobID)); uerr != nil {
				return Run{}, fmt.Errorf("runs: record job id: %w", uerr)
			}
			s.wg.Add(1)
			go s.watch(runID, adapter, jobID)
			return s.Get(ctx, runID)
		}
	}
	// Engine refused (no adapter for the class, or StartJob failed): the
	// submitted audit row stands, the run finalizes failed.
	if ferr := s.finalize(ctx, runID, string(engine.StateFailed), err.Error(), nil, nil, nil); ferr != nil {
		return Run{}, fmt.Errorf("runs: finalize refused run: %w", ferr)
	}
	run, gerr := s.Get(ctx, runID)
	if gerr != nil {
		return Run{}, gerr
	}
	return run, fmt.Errorf("%w: %s", ErrEngine, err.Error())
}

// watch polls the engine until the job is terminal, mirroring state into
// the run row, then finalizes (run + audit `run.finished`).
func (s *Service) watch(runID int64, adapter engine.Adapter, jobID engine.JobID) {
	defer s.wg.Done()
	ctx := context.Background() // outlives the submitting request

	for {
		st, err := adapter.Status(ctx, jobID)
		if err != nil {
			s.finalizeLogged(ctx, runID, string(engine.StateFailed),
				"engine lost the job: "+err.Error(), nil, nil, nil)
			return
		}
		if st.State.Terminal() {
			s.finalizeLogged(ctx, runID, string(st.State), st.Error,
				timePtr(st.Started), timePtr(st.Finished), st.Artifact)
			return
		}
		if _, err := s.pool.Exec(ctx, `
			UPDATE run SET state = $2, started_at = $3, updated_at = now() WHERE id = $1`,
			runID, string(st.State), timePtr(st.Started)); err != nil {
			s.log.Error("run status mirror failed", "run", runID, "err", err.Error())
		}
		time.Sleep(s.PollInterval)
	}
}

func (s *Service) finalizeLogged(ctx context.Context, runID int64, state, errMsg string, started, finished *time.Time, artifact *engine.Artifact) {
	if err := s.finalize(ctx, runID, state, errMsg, started, finished, artifact); err != nil {
		s.log.Error("run finalize failed", "run", runID, "err", err.Error())
	}
}

// finalize writes the terminal run state and appends the `run.finished`
// audit event, reusing the submitted event's immutable stamps.
func (s *Service) finalize(ctx context.Context, runID int64, state, errMsg string, started, finished *time.Time, artifact *engine.Artifact) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("runs: begin finalize: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var name, checksum *string
	var size *int64
	if artifact != nil {
		name, size, checksum = &artifact.Name, &artifact.SizeBytes, &artifact.Checksum
	}
	if finished == nil {
		now := time.Now().UTC()
		finished = &now
	}
	if _, err := tx.Exec(ctx, `
		UPDATE run SET state = $2, error = NULLIF($3, ''),
			started_at = COALESCE($4, started_at), finished_at = $5,
			artifact_name = $6, artifact_size_bytes = $7, artifact_checksum = $8,
			updated_at = now()
		WHERE id = $1`,
		runID, state, errMsg, started, finished, name, size, checksum); err != nil {
		return fmt.Errorf("runs: finalize run: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest, final_status)
		SELECT $1, 'run.finished', run_id, instance_id, environment, playbook_tag, params_digest, $3
		FROM audit_event WHERE run_id = $2 AND action = 'run.submitted'`,
		actor, runID, state); err != nil {
		return fmt.Errorf("runs: audit finish: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("runs: commit finalize: %w", err)
	}
	return nil
}

// SweepOrphans finalizes runs that were still live when the previous
// process died (SPEC-012 mini-ADR 5). Call once at startup, before serving.
func (s *Service) SweepOrphans(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM run WHERE state IN ('queued', 'running')`)
	if err != nil {
		return 0, fmt.Errorf("runs: sweep query: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("runs: sweep scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("runs: sweep: %w", err)
	}

	for _, id := range ids {
		if err := s.finalize(ctx, id, string(engine.StateFailed),
			"engine job lost (portal restart)", nil, nil, nil); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// paramsDigest is the sha256 hex of the canonical (sorted-key JSON) params —
// the audit trail's tamper-evident summary of what was sent to the engine.
func paramsDigest(params map[string]string) string {
	b, _ := json.Marshal(params) // map keys marshal sorted; cannot fail
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
