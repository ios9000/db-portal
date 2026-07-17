// Package chain implements portal-level sequential step execution over runs
// (D5, SPEC-032): steps fire strictly one after another through
// runs.Service.Start — the identical guardrail + audit path as the launch
// button and the scheduler — halt the chain on any not-success ending, and
// resume from the failed step on a DBA's explicit ask. This package never
// touches the engine: steps ARE runs.
package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ios9000/db-portal/backend/internal/catalog"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

var (
	// ErrNotFound is returned for an unknown chain id, and by ForRun for a
	// run that is not a chain step.
	ErrNotFound = errors.New("chain: not found")
	// ErrNotResumable means the chain is not halted: resume is single-flight
	// per chain (SPEC-032 behavior 4) — the guarded state flip admits one.
	ErrNotResumable = errors.New("chain: not halted")
	// ErrNoSteps rejects creating a chain with an empty step list.
	ErrNoSteps = errors.New("chain: a chain needs at least one step")
)

// Chain state vocabulary (CHECK-constrained in 0010). No terminal failure:
// chains halt, resumable indefinitely (mini-ADR 7).
const (
	stateRunning = "running"
	stateHalted  = "halted"
	stateSuccess = "success"
)

// Run states the driver reacts to — string twins of the CHECK-constrained
// run.state values, NOT engine imports (the grep-level seam rule).
const (
	runSuccess  = "success"
	runFailed   = "failed"
	runCanceled = "canceled"
)

func runTerminal(state string) bool {
	return state == runSuccess || state == runFailed || state == runCanceled
}

// opRestore is the restore operation id — a string twin, not a restore-package
// import (which would cycle: restore imports chain). Same seam rule as the run
// states above.
const opRestore = "restore"

func (s *Service) isProtected(instance string) bool {
	return s.Protected[strings.ToLower(instance)]
}

// hasRestoreStep reports whether any step is the (dangerous) restore op — the
// only step the Patroni block refuses (SPEC-042 mini-ADR 6). Dumps, including
// the chain's own safety dump, stay allowed on Patroni.
func hasRestoreStep(steps []StepSpec) bool {
	for _, st := range steps {
		if st.Operation == opRestore {
			return true
		}
	}
	return false
}

// Runs is the one door every step fires through — satisfied by
// *runs.Service, faked in driver tests.
type Runs interface {
	Start(ctx context.Context, req runs.StartRequest) (runs.Run, error)
	Get(ctx context.Context, id int64) (runs.Run, error)
}

// Notifier is told about every halt, exactly once per halt transition
// (mini-ADR 5). Implementations follow the D7 content rule: who/what/where/
// status + link only. Satisfied by *notify.Mailer.
type Notifier interface {
	ChainHalted(ctx context.Context, c Chain) error
}

// Step is the API read model of one chain step. Status is derived from the
// linked run — `pending` while run_id is NULL, the run's state after
// (mini-ADR 4).
type Step struct {
	Seq       int    `json:"seq"`
	Operation string `json:"operation"`
	RunID     *int64 `json:"run_id"`
	Status    string `json:"status"`
}

// Chain is the API read model. Env rides along from the instance join (the
// UI's env banner); steps come ordered by seq.
type Chain struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	Instance   string     `json:"instance"`
	Env        string     `json:"env"`
	State      string     `json:"state"`
	CreatedBy  string     `json:"created_by"`
	Reason     *string    `json:"reason"`
	CreatedAt  time.Time  `json:"created_at"`
	HaltedAt   *time.Time `json:"halted_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Steps      []Step     `json:"steps"`
}

// StepSpec declares one step at creation: a catalog operation plus the
// engine params its run fires with (031's restore steps carry artifact
// refs here — ids and checksums, never secrets).
type StepSpec struct {
	Operation string
	Params    map[string]string
}

// CreateRequest is one chain ask. Actor is the initiating identity —
// explicit, never context magic (SPEC-021 mini-ADR 4). Confirm is the prod
// ritual, performed once at creation and PERSISTED: every step fires with
// it verbatim (mini-ADR 3, the schedule.confirm port). Reason propagates to
// every step run.
type CreateRequest struct {
	Kind     string
	Instance string
	Actor    string
	Confirm  string
	Reason   string
	Steps    []StepSpec
}

// Service is the chain store + driver. Safe for concurrent use.
type Service struct {
	pool *pgxpool.Pool
	runs Runs
	log  *slog.Logger

	// PollInterval is the driver's run-status poll cadence. Set before
	// first use (tests use ~1ms).
	PollInterval time.Duration

	// MaxReadErrors bounds how many consecutive TRANSIENT read failures the
	// driver tolerates on any of its DB polls before giving up honestly —
	// halting the chain (which notifies) rather than silently wedging it
	// 'running' with no self-heal short of a restart (WU-037 / m3-gate
	// finding 2). Set before first use.
	MaxReadErrors int

	// Notifier, when non-nil, is told about halts after the guarded
	// transition commits. Best-effort: errors are logged, never propagated.
	// Set before first use.
	Notifier Notifier

	// Protected is the self-target ban's denylist (SPEC-042 mini-ADR 5),
	// instance names lowercased — the same set runs.Service carries, so the
	// restore path refuses synchronously in Create rather than halting a step
	// later. nil/empty = off. Set before first use.
	Protected map[string]bool

	wg sync.WaitGroup
}

func New(pool *pgxpool.Pool, r Runs, log *slog.Logger) *Service {
	return &Service{
		pool: pool, runs: r, log: log,
		PollInterval: 500 * time.Millisecond, MaxReadErrors: defaultMaxReadErrors,
	}
}

// Wait blocks until every driver and notify goroutine has finished. Test
// helper; production never calls it (drivers die with the process, the
// boot sweep halts what they left running).
func (s *Service) Wait() { s.wg.Wait() }

// Create validates and stores a chain, then starts driving it. Reuses the
// runs error vocabulary where the check is the same one Start performs, so
// handlers answer identically (the schedule.Create pattern).
func (s *Service) Create(ctx context.Context, req CreateRequest) (Chain, error) {
	if len(req.Steps) == 0 {
		return Chain{}, ErrNoSteps
	}
	for _, st := range req.Steps {
		if _, ok := catalog.ByID(st.Operation); !ok {
			return Chain{}, fmt.Errorf("%w: %q", runs.ErrUnknownOperation, st.Operation)
		}
	}

	var instanceID int64
	var env, platform string
	err := s.pool.QueryRow(ctx, `
		SELECT i.id, i.env, c.platform
		FROM instance i JOIN cluster c ON c.id = i.cluster_id
		WHERE i.name = $1`, req.Instance).
		Scan(&instanceID, &env, &platform)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Chain{}, fmt.Errorf("%w: %q", runs.ErrUnknownInstance, req.Instance)
	case err != nil:
		return Chain{}, fmt.Errorf("chain: look up instance: %w", err)
	}
	// Target guardrails (SPEC-042), refused synchronously before any chain row
	// so the restore handler answers with the distinct error instead of an
	// async halt. Both record the denial on the security ledger.
	if s.isProtected(req.Instance) {
		if derr := runs.RecordGuardrailDenial(ctx, s.pool, req.Actor, "self-target", req.Instance); derr != nil {
			s.log.Error("guardrail denial write failed", "kind", "self-target", "err", derr.Error())
		}
		return Chain{}, fmt.Errorf("%w: %q", runs.ErrSelfTarget, req.Instance)
	}
	if platform == "k8s_patroni" && hasRestoreStep(req.Steps) {
		if derr := runs.RecordGuardrailDenial(ctx, s.pool, req.Actor, "patroni-restore", req.Instance); derr != nil {
			s.log.Error("guardrail denial write failed", "kind", "patroni-restore", "err", derr.Error())
		}
		return Chain{}, fmt.Errorf("%w: %q", runs.ErrPatroniRestore, req.Instance)
	}
	// The prod ritual happens once, here, for every step to come; the typed
	// confirm is persisted and fired verbatim so an instance promoted to
	// prod mid-chain fails the ritual visibly, never auto-confirms
	// (mini-ADR 3 — the schedule.confirm behavior, ported).
	if env == "prod" && req.Confirm != req.Instance {
		return Chain{}, runs.ErrProdUnconfirmed
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Chain{}, fmt.Errorf("chain: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO chain (kind, instance_id, created_by, confirm, reason)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''))
		RETURNING id`,
		req.Kind, instanceID, req.Actor, req.Confirm, req.Reason).Scan(&id)
	if err != nil {
		return Chain{}, fmt.Errorf("chain: insert chain: %w", err)
	}
	for i, st := range req.Steps {
		params := st.Params
		if params == nil {
			params = map[string]string{} // jsonb '{}', never 'null'
		}
		buf, err := json.Marshal(params)
		if err != nil {
			return Chain{}, fmt.Errorf("chain: marshal step params: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO chain_step (chain_id, seq, operation, params)
			VALUES ($1, $2, $3, $4)`,
			id, i+1, st.Operation, buf); err != nil {
			return Chain{}, fmt.Errorf("chain: insert step: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Chain{}, fmt.Errorf("chain: commit: %w", err)
	}

	s.wg.Add(1)
	go s.drive(id, req.Actor)
	return s.Get(ctx, id)
}

// Resume restarts a halted chain: the failed step re-fires as a NEW run
// (the superseded run keeps its rows — history lives in run/audit) and the
// remaining steps continue. Actor is the resumer's session identity; the
// re-fired runs are attributed chain:<resumer>, not the creator (mini-ADR
// 1). The guarded flip is the single-flight lock: a concurrent resume, or
// one on a running chain, affects 0 rows and gets ErrNotResumable.
func (s *Service) Resume(ctx context.Context, id int64, actor string) (Chain, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE chain SET state = 'running', halted_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'halted'`, id)
	if err != nil {
		return Chain{}, fmt.Errorf("chain: resume flip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var state string
		err := s.pool.QueryRow(ctx, `SELECT state FROM chain WHERE id = $1`, id).Scan(&state)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return Chain{}, ErrNotFound
		case err != nil:
			return Chain{}, fmt.Errorf("chain: resume check: %w", err)
		}
		return Chain{}, fmt.Errorf("%w (state %q)", ErrNotResumable, state)
	}

	s.wg.Add(1)
	go s.drive(id, actor)
	return s.Get(ctx, id)
}

const chainColumns = `
	SELECT c.id, c.kind, i.name, i.env, c.state, c.created_by, c.reason,
		c.created_at, c.halted_at, c.finished_at
	FROM chain c JOIN instance i ON i.id = c.instance_id`

// Get returns one chain with its steps, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id int64) (Chain, error) {
	var c Chain
	err := s.pool.QueryRow(ctx, chainColumns+` WHERE c.id = $1`, id).
		Scan(&c.ID, &c.Kind, &c.Instance, &c.Env, &c.State, &c.CreatedBy,
			&c.Reason, &c.CreatedAt, &c.HaltedAt, &c.FinishedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Chain{}, ErrNotFound
	case err != nil:
		return Chain{}, fmt.Errorf("chain: get: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT cs.seq, cs.operation, cs.run_id,
			CASE WHEN cs.run_id IS NULL THEN 'pending' ELSE r.state END
		FROM chain_step cs LEFT JOIN run r ON r.id = cs.run_id
		WHERE cs.chain_id = $1 ORDER BY cs.seq`, id)
	if err != nil {
		return Chain{}, fmt.Errorf("chain: steps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st Step
		if err := rows.Scan(&st.Seq, &st.Operation, &st.RunID, &st.Status); err != nil {
			return Chain{}, fmt.Errorf("chain: scan step: %w", err)
		}
		c.Steps = append(c.Steps, st)
	}
	if err := rows.Err(); err != nil {
		return Chain{}, fmt.Errorf("chain: steps: %w", err)
	}
	return c, nil
}

// ForRun finds the chain a run is a step of — the RunDetail strip lookup.
// ErrNotFound for unknown runs and for runs that are not chain steps (same
// answer: the strip simply isn't shown).
func (s *Service) ForRun(ctx context.Context, runID int64) (Chain, error) {
	var chainID int64
	err := s.pool.QueryRow(ctx,
		`SELECT chain_id FROM chain_step WHERE run_id = $1`, runID).Scan(&chainID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Chain{}, ErrNotFound
	case err != nil:
		return Chain{}, fmt.Errorf("chain: for run: %w", err)
	}
	return s.Get(ctx, chainID)
}

// SweepOrphans halts chains left `running` by a dead process (SPEC-032
// behavior 6), notifying once each. Call at startup AFTER runs.SweepOrphans
// — the runs sweep fails the orphaned step runs first, so "no live linked
// run" is decisive here. Halted chains never auto-continue: a step can't
// double-fire across a restart because only a human resume moves them.
func (s *Service) SweepOrphans(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id FROM chain c
		WHERE c.state = 'running'
		AND NOT EXISTS (
			SELECT 1 FROM chain_step cs JOIN run r ON r.id = cs.run_id
			WHERE cs.chain_id = c.id AND r.state IN ('queued', 'running'))`)
	if err != nil {
		return 0, fmt.Errorf("chain: sweep query: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("chain: sweep scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("chain: sweep: %w", err)
	}

	// Per-chain best effort, like the runs sweep (m1-gate item 9).
	swept := 0
	for _, id := range ids {
		if err := s.halt(ctx, id); err != nil {
			s.log.Error("chain sweep: halt failed", "chain", id, "err", err.Error())
			continue
		}
		swept++
	}
	return swept, nil
}

// StepRunFilter suppresses run-level failure mail for chain step runs, so a
// halt produces exactly ONE mail — the chain's (mini-ADR 5). Keying on the
// attribution prefix is race-free: it is stamped on the run.submitted row
// before any finalize can notify, unlike the chain_step linkage which lands
// only after Start returns. main wires this around the Mailer.
type StepRunFilter struct {
	Next runs.Notifier
}

func (f StepRunFilter) RunEnded(ctx context.Context, run runs.Run) error {
	if strings.HasPrefix(run.RequestedBy, "chain:") {
		return nil // the halt path mails with chain context instead
	}
	return f.Next.RunEnded(ctx, run)
}
