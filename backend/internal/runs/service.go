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
	"github.com/ios9000/db-portal/backend/internal/window"
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
	// ErrNoLogs means the run's logs cannot be streamed: no engine job was
	// ever started, or the engine no longer knows the job (post-restart
	// orphans). Logs are not persisted — SPEC-013 mini-ADR 3.
	ErrNoLogs = errors.New("runs: logs unavailable")
	// ErrNotCancelable means the run is already terminal or its engine job
	// is lost — there is nothing left to cancel.
	ErrNotCancelable = errors.New("runs: run is not cancelable")
	// ErrProdUnconfirmed is the server side of the prod ritual (SPEC-021
	// mini-ADR 6): a prod launch whose Confirm doesn't name the instance.
	ErrProdUnconfirmed = errors.New("runs: prod launch requires typing the instance name")
)

// Notifier receives a copy of every run that ends not-success (SPEC-014).
// Implementations MUST NOT include params, reason or log content in what
// they send (D7 / ARCHITECTURE §2: who/what/where/status + link only).
type Notifier interface {
	RunEnded(ctx context.Context, run Run) error
}

// Service launches and tracks runs. Safe for concurrent use.
type Service struct {
	pool     *pgxpool.Pool
	registry *engine.Registry
	log      *slog.Logger

	// PollInterval is the watcher's engine-status poll cadence. Set before
	// first use (tests use ~1ms; default suits the dev mock).
	PollInterval time.Duration

	// MaxStatusErrors bounds how many consecutive TRANSIENT engine Status
	// failures the watcher tolerates before giving up honestly (finalize
	// failed with an accurate message). A real adapter's Status returns
	// conn-refused / 5xx / timeout / decode errors that MockEngine never
	// did (WU-037 / m3-gate finding 1); those are retried with backoff, not
	// mistaken for a permanent "job lost". Set before first use.
	MaxStatusErrors int

	// Notifier, when non-nil, is told about failed/canceled runs after
	// they finalize. Best-effort: errors are logged, never propagated.
	// Set before first use.
	Notifier Notifier

	// failRecordJobID (tests only, via export_test.go) forces the
	// post-StartJob job-id record to fail so the stranded-job repair
	// path is exercisable deterministically.
	failRecordJobID error

	// warnedWindows guards the once-per-instance-per-process log for
	// unparseable maintenance windows (SPEC-023 mini-ADR 3) — garbage
	// window text must not spam a line per launch.
	warnedWindows sync.Map

	wg sync.WaitGroup
}

func NewService(pool *pgxpool.Pool, registry *engine.Registry, log *slog.Logger) *Service {
	return &Service{
		pool: pool, registry: registry, log: log,
		PollInterval: 500 * time.Millisecond, MaxStatusErrors: defaultMaxStatusErrors,
	}
}

// Wait blocks until every watcher goroutine has finished. Test helper;
// production never calls it (watchers die with the process, the orphan
// sweep repairs on next boot).
func (s *Service) Wait() { s.wg.Wait() }

// StartRequest is one launch ask. Actor is the audit identity — explicit,
// never context magic (SPEC-021 mini-ADR 4): a missed middleware must fail
// loudly at the call site, not silently audit "". WU-022's scheduler passes
// `schedule:<owner>` through the same door. Confirm is the server side of
// the prod ritual; non-prod ignores it. EngineParams is for tests and
// future parameterized ops; the API passes nil (MVP accepts no client params).
// Internal permits a non-launchable operation (a chain step — verify /
// safety_dump / restore, SPEC-031 mini-ADR 3); ONLY the chain driver sets it,
// so a bare restore over POST /api/runs or the scheduler is refused at the door.
type StartRequest struct {
	Actor        string
	Instance     string
	Operation    string
	Reason       string
	Confirm      string
	Internal     bool
	EngineParams map[string]string
}

// Start launches the requested operation: run row + audit `run.submitted`
// first (one tx), then the engine job via the Registry. Engine refusal
// finalizes the run failed and returns ErrEngine — the audit trail records
// the attempt either way.
func (s *Service) Start(ctx context.Context, req StartRequest) (Run, error) {
	op, ok := catalog.ByID(req.Operation)
	if !ok {
		return Run{}, fmt.Errorf("%w: %q", ErrUnknownOperation, req.Operation)
	}
	// The launchable gate (SPEC-031 mini-ADR 3): internal chain-step
	// operations reach Start only through the chain driver (Internal set).
	// A bare restore/verify/safety_dump over the button or scheduler looks
	// like an unknown operation — the single choke point for AC-4.
	if !op.Launchable && !req.Internal {
		return Run{}, fmt.Errorf("%w: %q", ErrUnknownOperation, req.Operation)
	}

	var instanceID int64
	var env string
	var maintenanceWindow *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, env, maintenance_window FROM instance WHERE name = $1`, req.Instance).
		Scan(&instanceID, &env, &maintenanceWindow)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Run{}, fmt.Errorf("%w: %q", ErrUnknownInstance, req.Instance)
	case err != nil:
		return Run{}, fmt.Errorf("runs: look up instance: %w", err)
	}

	// The prod ritual, enforced where env is authoritative (SPEC-021 mini-
	// ADR 6): same exact-match predicate as the drawer. Checked before any
	// row is written — a failed ritual is friction, not a security event.
	if env == "prod" && req.Confirm != req.Instance {
		return Run{}, ErrProdUnconfirmed
	}

	windowWarned := s.outsideWindow(req.Instance, maintenanceWindow)

	class, err := engine.ClassForEnv(env) // fails closed on garbage
	if err != nil {
		return Run{}, fmt.Errorf("runs: %w", err)
	}

	params := map[string]string{"instance": req.Instance}
	for k, v := range req.EngineParams {
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
		instanceID, op.ID, env, string(class), op.PlaybookTag, req.Reason).Scan(&runID)
	if err != nil {
		return Run{}, fmt.Errorf("runs: insert run: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest, window_warned)
		VALUES ($1, 'run.submitted', $2, $3, $4, $5, $6, $7)`,
		req.Actor, runID, instanceID, env, op.PlaybookTag, digest, windowWarned); err != nil {
		return Run{}, fmt.Errorf("runs: audit submit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, fmt.Errorf("runs: commit: %w", err)
	}

	adapter, err := s.registry.For(class)
	if err == nil {
		var jobID engine.JobID
		if jobID, err = adapter.StartJob(ctx, op.Template, params); err == nil {
			if uerr := s.recordJobID(ctx, runID, jobID); uerr != nil {
				// The engine job is live but the portal can't track it:
				// cancel it (best effort) and finalize failed so the run
				// never sits 'queued' forever behind a running job
				// (SPEC-012 mini-ADR 7). Fresh context — ctx may be the
				// very reason the UPDATE failed.
				rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if cerr := adapter.Cancel(rctx, jobID); cerr != nil {
					s.log.Error("stranded job cancel failed",
						"run", runID, "job", string(jobID), "err", cerr.Error())
				}
				s.finalizeLogged(rctx, runID, string(engine.StateFailed),
					"portal failed to record the engine job: "+uerr.Error(), nil, nil, nil)
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

// outsideWindow reports whether now falls outside the instance's
// maintenance window (SPEC-023): the audit stamp both launch paths share,
// since drawer and scheduler flow through Start. Warn-only semantics (D6):
// no window, or a window the parser rejects, is simply false — a window
// can never block a launch. Unparseable text logs once per instance per
// process, not per launch.
func (s *Service) outsideWindow(instance string, raw *string) bool {
	if raw == nil || *raw == "" {
		return false
	}
	w, err := window.Parse(*raw)
	if err != nil {
		if _, logged := s.warnedWindows.LoadOrStore(instance, true); !logged {
			s.log.Warn("maintenance window unparseable — window warnings off for this instance",
				"instance", instance, "window", *raw, "err", err.Error())
		}
		return false
	}
	return !w.Contains(time.Now())
}

// recordJobID stores the engine job id on the run row — the link the
// watcher, log streaming and cancel all depend on. Failure here means a
// live job the portal can't see; Start repairs by canceling + finalizing.
func (s *Service) recordJobID(ctx context.Context, runID int64, jobID engine.JobID) error {
	if s.failRecordJobID != nil {
		return s.failRecordJobID
	}
	_, err := s.pool.Exec(ctx, `UPDATE run SET job_id = $2, updated_at = now() WHERE id = $1`,
		runID, string(jobID))
	return err
}

// defaultMaxStatusErrors is the watcher's tolerance for consecutive transient
// engine Status failures before an honest give-up (WU-037). With the semaphore
// poll cadence (3s) backing off toward statusBackoffCap, this spans a
// multi-minute engine/proxy outage — long enough that a brief blip or failover
// never falsely fails a run, bounded so a genuinely unreachable engine still
// resolves the run rather than watching forever.
const defaultMaxStatusErrors = 30

// statusBackoffCap ceilings the exponential retry delay during an outage so the
// watcher keeps probing often enough to notice recovery.
const statusBackoffCap = 30 * time.Second

// watch polls the engine until the job is terminal, mirroring state into
// the run row, then finalizes (run + audit `run.finished`).
func (s *Service) watch(runID int64, adapter engine.Adapter, jobID engine.JobID) {
	defer s.wg.Done()
	ctx := context.Background() // outlives the submitting request

	statusErrs := 0
	for {
		st, err := adapter.Status(ctx, jobID)
		if err != nil {
			// ErrUnknownJob is a permanent, correct failure — the job the
			// engine genuinely no longer knows (the mock's ONLY error, and a
			// real adapter's 404/400). Any OTHER error is transient
			// (conn-refused, a 5xx, a timeout, a decode failure): under
			// MockEngine "any error == job lost" held, but a real engine behind
			// the unchanged seam turned a single blip into a FALSE, permanent,
			// uncorrectable FAILED with a lying "engine lost the job" message
			// (m3-gate finding 1). Retry those with backoff — mirroring the
			// state-mirror UPDATE below, which already tolerates a DB error —
			// giving up honestly only after a sustained outage.
			if errors.Is(err, engine.ErrUnknownJob) {
				s.finalizeLogged(ctx, runID, string(engine.StateFailed),
					"engine lost the job: "+err.Error(), nil, nil, nil)
				return
			}
			statusErrs++
			if statusErrs >= s.MaxStatusErrors {
				s.finalizeLogged(ctx, runID, string(engine.StateFailed),
					fmt.Sprintf("engine status unavailable after %d attempts: %s", statusErrs, err.Error()),
					nil, nil, nil)
				return
			}
			s.log.Warn("run status poll failed, retrying",
				"run", runID, "attempt", statusErrs, "err", err.Error())
			time.Sleep(backoff(s.PollInterval, statusBackoffCap, statusErrs))
			continue
		}
		statusErrs = 0 // a reachable engine resets the outage counter
		if st.State.Terminal() {
			s.finalizeLogged(ctx, runID, string(st.State), st.Error,
				timePtr(st.Started), timePtr(st.Finished), st.Artifact)
			return
		}
		// Same terminal guard as finalize: an unguarded mirror would
		// resurrect a run that a competing finalizer (cancel race, sweep)
		// already closed (SPEC-012 mini-ADR 8).
		tag, err := s.pool.Exec(ctx, `
			UPDATE run SET state = $2, started_at = $3, updated_at = now()
			WHERE id = $1 AND state NOT IN ('success', 'failed', 'canceled')`,
			runID, string(st.State), timePtr(st.Started))
		switch {
		case err != nil:
			s.log.Error("run status mirror failed", "run", runID, "err", err.Error())
		case tag.RowsAffected() == 0:
			// The run was finalized under us; its outcome stands. Nothing
			// left to watch.
			return
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
	tag, err := tx.Exec(ctx, `
		UPDATE run SET state = $2, error = NULLIF($3, ''),
			started_at = COALESCE($4, started_at), finished_at = $5,
			artifact_name = $6, artifact_size_bytes = $7, artifact_checksum = $8,
			updated_at = now()
		WHERE id = $1 AND state NOT IN ('success', 'failed', 'canceled')`,
		runID, state, errMsg, started, finished, name, size, checksum)
	if err != nil {
		return fmt.Errorf("runs: finalize run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Already terminal: another finalizer (watcher vs cancel vs sweep)
		// won the race. The first outcome stands — no duplicate audit
		// event, no notification (SPEC-012 mini-ADR 8).
		return nil
	}

	// Register the artifact (SPEC-030): same tx as the guarded run UPDATE,
	// so the registry row and the run's artifact_* columns cannot diverge
	// and a losing finalizer (0 rows above) never reaches this INSERT.
	// UNIQUE (run_id) backstops exactly-once at the schema layer. The
	// retention class rides the run's operation (SPEC-031 mini-ADR 2): a
	// safety_dump stamps 'safety', a plain dump 'standard' — read from the
	// catalog, defaulting to 'standard' for any operation that omits it.
	if state == string(engine.StateSuccess) && artifact != nil {
		var operation string
		if err := tx.QueryRow(ctx,
			`SELECT operation FROM run WHERE id = $1`, runID).Scan(&operation); err != nil {
			return fmt.Errorf("runs: read run operation: %w", err)
		}
		class := "standard"
		if op, ok := catalog.ByID(operation); ok && op.RetentionClass != "" {
			class = op.RetentionClass
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO artifact (run_id, name, size_bytes, checksum, retention_class, location)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
			runID, artifact.Name, artifact.SizeBytes, artifact.Checksum, class, artifact.Location); err != nil {
			return fmt.Errorf("runs: register artifact: %w", err)
		}
	}

	// Actor is inherited from the submitted row along with the other
	// immutable stamps: it means "on whose behalf", not "which component
	// wrote the row" — watcher and sweep finalizations carry the requester
	// (SPEC-021 mini-ADR 4).
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest, final_status, job_id)
		SELECT ae.actor, 'run.finished', ae.run_id, ae.instance_id, ae.environment, ae.playbook_tag, ae.params_digest, $2, r.job_id
		FROM audit_event ae JOIN run r ON r.id = ae.run_id
		WHERE ae.run_id = $1 AND ae.action = 'run.submitted'`,
		runID, state); err != nil {
		return fmt.Errorf("runs: audit finish: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("runs: commit finalize: %w", err)
	}
	s.notifyEnded(runID, state)
	return nil
}

// notifyEnded mails the DBA list about a not-success ending (SPEC-014
// mini-ADRs 1+2): fired only after finalize committed, in a tracked
// goroutine so a slow or dead SMTP host never blocks finalization.
func (s *Service) notifyEnded(runID int64, state string) {
	if s.Notifier == nil ||
		(state != string(engine.StateFailed) && state != string(engine.StateCanceled)) {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx := context.Background() // outlives the finalizing caller
		run, err := s.Get(ctx, runID)
		if err != nil {
			s.log.Error("notify: load run", "run", runID, "err", err.Error())
			return
		}
		if err := s.Notifier.RunEnded(ctx, run); err != nil {
			s.log.Error("run notification failed", "run", runID, "err", err.Error())
		}
	}()
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

	// Per-run best effort: one broken run must not leave every other
	// orphan stuck live for the process lifetime (m1-gate item 9).
	swept := 0
	for _, id := range ids {
		if err := s.finalize(ctx, id, string(engine.StateFailed),
			"engine job lost (portal restart)", nil, nil, nil); err != nil {
			s.log.Error("orphan sweep: finalize failed", "run", id, "err", err.Error())
			continue
		}
		swept++
	}
	return swept, nil
}

// ReconcileByJobID accelerates one run to its terminal state (SPEC-033
// mini-ADR 2): it finds the run carrying jobID and runs ONE Status→finalize.
// It is the Semaphore webhook's only entry into runs, and it asserts NOTHING
// from the webhook — the outcome comes from re-polling the REAL engine job,
// whose status is authoritative. Poll stays the truth: the periodic watcher
// runs this identical path, so a missing webhook still finalizes (the ADR-002
// fallback) and a payload that lies about status cannot force an outcome.
//
// No-op (nil) when nothing needs doing: no run owns that job id
// (stale/spoofed/unknown task), the run is already terminal, the adapter no
// longer knows the job (ErrUnknownJob — a genuinely lost job is the poll
// authority's call, not the webhook's), or the job is not yet terminal. Only a
// terminal Status finalizes, and the WU-016 single-finalizer guard makes that
// safe alongside the concurrent watcher — whoever wins finalizes, the other's
// UPDATE touches 0 rows and is a silent no-op (no duplicate audit or notify).
func (s *Service) ReconcileByJobID(ctx context.Context, jobID string) error {
	if jobID == "" {
		return nil
	}
	var runID int64
	var class engine.EnvClass
	var state string
	err := s.pool.QueryRow(ctx,
		`SELECT id, engine_class, state FROM run WHERE job_id = $1 ORDER BY id DESC LIMIT 1`, jobID).
		Scan(&runID, &class, &state)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil // no run owns this task id — nothing to reconcile
	case err != nil:
		return fmt.Errorf("runs: reconcile look up job %q: %w", jobID, err)
	}
	if engine.JobState(state).Terminal() {
		return nil // already finalized; the first outcome stands
	}
	adapter, err := s.registry.For(class)
	if err != nil {
		return fmt.Errorf("runs: reconcile job %q: %w", jobID, err)
	}
	st, err := adapter.Status(ctx, engine.JobID(jobID))
	if errors.Is(err, engine.ErrUnknownJob) {
		return nil // the poll authority owns a genuinely lost job, not the webhook
	}
	if err != nil {
		return fmt.Errorf("runs: reconcile status job %q: %w", jobID, err)
	}
	if !st.State.Terminal() {
		return nil // not done yet — the periodic watcher keeps polling
	}
	return s.finalize(ctx, runID, string(st.State), st.Error,
		timePtr(st.Started), timePtr(st.Finished), st.Artifact)
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

// backoff returns base, doubled once per prior attempt, ceilinged at limit —
// the retry cadence for transient engine errors (WU-037). attempt is 1-based
// (attempt 1 waits base). Overflow-safe: it returns limit as soon as the
// doubling reaches it.
func backoff(base, limit time.Duration, attempt int) time.Duration {
	d := base
	for i := 1; i < attempt; i++ {
		if d >= limit {
			return limit
		}
		d *= 2
	}
	if d > limit {
		return limit
	}
	return d
}
