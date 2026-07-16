package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ios9000/db-portal/backend/internal/runs"
)

// driveStep is the driver's working view of the step it must move next:
// the lowest seq whose run is missing or ended not-success.
type driveStep struct {
	id        int64
	seq       int
	operation string
	params    map[string]string
	runID     *int64
	runState  *string
}

// defaultMaxReadErrors is the driver's tolerance for consecutive transient DB
// read failures before an honest give-up (WU-037). Backing off toward
// readBackoffCap, it spans a multi-minute portal-DB blip/failover so a chain
// rides through one, bounded so a sustained outage self-halts (with mail +
// resume) instead of wedging 'running' forever.
const defaultMaxReadErrors = 30

// readBackoffCap ceilings the driver's exponential retry delay during an outage.
const readBackoffCap = 15 * time.Second

// drive advances one chain to success or halt: fire the current step
// through runs.Start, watch its run to terminal, repeat (SPEC-032 mini-ADR
// 6 — create and resume share this loop; mover is whoever set THIS pass in
// motion, so step runs audit as chain:<creator> or chain:<resumer>).
// Fire-time errors halt visibly instead of surfacing to a caller — the
// scheduler's last_fire_status='error' posture. A transient DB read error is
// retried with backoff (WU-037): under a real deployment the portal DB can
// blip or fail over mid-chain, and the boot sweep is boot-ONLY (no ticker) —
// so exiting the loop on the first read error left the chain wedged 'running'
// with no alert and no resume path (Resume guards state='halted'); m3-gate
// finding 2. When retries are exhausted the driver halts the chain honestly
// (which notifies and re-enables Resume), never a silent exit. A crash still
// leaves the chain for the boot sweep — that path is unchanged.
func (s *Service) drive(chainID int64, mover string) {
	defer s.wg.Done()
	ctx := context.Background() // outlives the creating/resuming request

	instance, confirm, reason, err := s.loadChain(ctx, chainID)
	if err != nil {
		s.log.Error("chain: driver load failed after retries, halting", "chain", chainID, "err", err.Error())
		s.haltLogged(ctx, chainID)
		return
	}

	for {
		step, status, err := s.nextRetry(ctx, chainID)
		if err != nil {
			s.log.Error("chain: next step lookup failed after retries, halting", "chain", chainID, "err", err.Error())
			s.haltLogged(ctx, chainID)
			return
		}
		if status == driveStopped {
			return // halted or finished under us; that outcome stands
		}
		if status == driveComplete {
			s.finishLogged(ctx, chainID)
			return
		}

		var runID int64
		if step.runID != nil && !runTerminal(*step.runState) {
			// A live linked run: adopt and watch it (defensive — the
			// single-flight flip means no second driver fired it).
			runID = *step.runID
		} else {
			// Fire: pending step, or a failed/canceled one being resumed —
			// a NEW run either way, with the stored ritual evidence and the
			// chain's reason, attributed to this pass's mover.
			var reasonStr string
			if reason != nil {
				reasonStr = *reason
			}
			run, err := s.runs.Start(ctx, runs.StartRequest{
				Actor:     "chain:" + mover,
				Instance:  instance,
				Operation: step.operation,
				Reason:    reasonStr,
				Confirm:   confirm,
				// Chain steps may be internal operations (restore's verify /
				// safety_dump / restore, SPEC-031 mini-ADR 3); the driver is
				// the one caller allowed to fire them.
				Internal:     true,
				EngineParams: step.params,
			})
			if run.ID != 0 {
				// Present even on ErrEngine (the run exists, finalized
				// failed): link it so the strip and the mail can point at it.
				if lerr := s.linkStep(ctx, step.id, run.ID); lerr != nil {
					s.log.Error("chain: step link failed", "chain", chainID, "step", step.seq, "err", lerr.Error())
					return
				}
			}
			if err != nil {
				// Ritual refusal (env promoted mid-chain — no run row) or
				// engine refusal: halt visibly, never auto-confirm.
				s.log.Warn("chain: step fire failed, halting",
					"chain", chainID, "step", step.seq, "err", err.Error())
				s.haltLogged(ctx, chainID)
				return
			}
			runID = run.ID
		}

		runState, err := s.watchRun(ctx, runID)
		if err != nil {
			s.log.Error("chain: step watch failed after retries, halting", "chain", chainID, "run", runID, "err", err.Error())
			s.haltLogged(ctx, chainID)
			return
		}
		if runState != runSuccess {
			s.haltLogged(ctx, chainID)
			return
		}
	}
}

// loadChain reads the driver's per-chain constants (target, stored ritual
// confirm, reason), retrying transient DB errors with backoff up to
// MaxReadErrors (WU-037).
func (s *Service) loadChain(ctx context.Context, chainID int64) (instance, confirm string, reason *string, err error) {
	for attempt := 1; ; attempt++ {
		err = s.pool.QueryRow(ctx, `
			SELECT i.name, c.confirm, c.reason
			FROM chain c JOIN instance i ON i.id = c.instance_id
			WHERE c.id = $1`, chainID).Scan(&instance, &confirm, &reason)
		if err == nil || attempt >= s.MaxReadErrors {
			return instance, confirm, reason, err
		}
		s.log.Warn("chain: driver load failed, retrying", "chain", chainID, "attempt", attempt, "err", err.Error())
		time.Sleep(backoff(s.PollInterval, readBackoffCap, attempt))
	}
}

// nextRetry wraps next() with the same transient-read tolerance: a DB error
// (not a driveStopped/driveComplete verdict, which carry a nil error) retries
// with backoff before the driver gives up and halts (WU-037).
func (s *Service) nextRetry(ctx context.Context, chainID int64) (driveStep, driveStatus, error) {
	for attempt := 1; ; attempt++ {
		step, status, err := s.next(ctx, chainID)
		if err == nil {
			return step, status, nil
		}
		if attempt >= s.MaxReadErrors {
			return driveStep{}, driveStopped, err
		}
		s.log.Warn("chain: next step lookup failed, retrying", "chain", chainID, "attempt", attempt, "err", err.Error())
		time.Sleep(backoff(s.PollInterval, readBackoffCap, attempt))
	}
}

// watchRun polls one step's run to a terminal state, returning that state. A
// transient read error retries with backoff (a successful read resets the
// counter, so blips don't accumulate across a long step); MaxReadErrors
// consecutive failures return the error so the driver halts honestly (WU-037).
func (s *Service) watchRun(ctx context.Context, runID int64) (string, error) {
	errs := 0
	for {
		run, err := s.runs.Get(ctx, runID)
		if err != nil {
			errs++
			if errs >= s.MaxReadErrors {
				return "", err
			}
			s.log.Warn("chain: step watch read failed, retrying", "run", runID, "attempt", errs, "err", err.Error())
			time.Sleep(backoff(s.PollInterval, readBackoffCap, errs))
			continue
		}
		errs = 0
		if runTerminal(run.State) {
			return run.State, nil
		}
		time.Sleep(s.PollInterval)
	}
}

// backoff returns base, doubled once per prior attempt, ceilinged at limit —
// the retry cadence for transient reads (WU-037). attempt is 1-based (attempt
// 1 waits base). Overflow-safe: it returns limit as soon as the doubling
// reaches it. (A twin of runs.backoff; the packages stay decoupled by design.)
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

// driveStatus is next()'s verdict on the chain's advance.
type driveStatus int

const (
	driveAdvance  driveStatus = iota // the returned step needs firing/watching
	driveComplete                    // every step succeeded — finish the chain
	driveStopped                     // no longer running (swept/halted) — stop driving
)

// next returns the lowest step whose run is missing or not-success, or
// driveComplete when every step succeeded. It also re-checks the chain is
// still `running` — a swept or externally halted chain must not be driven on.
func (s *Service) next(ctx context.Context, chainID int64) (driveStep, driveStatus, error) {
	var state string
	if err := s.pool.QueryRow(ctx,
		`SELECT state FROM chain WHERE id = $1`, chainID).Scan(&state); err != nil {
		return driveStep{}, driveStopped, fmt.Errorf("chain state: %w", err)
	}
	if state != stateRunning {
		return driveStep{}, driveStopped, nil
	}

	var st driveStep
	var buf []byte
	err := s.pool.QueryRow(ctx, `
		SELECT cs.id, cs.seq, cs.operation, cs.params, cs.run_id, r.state
		FROM chain_step cs LEFT JOIN run r ON r.id = cs.run_id
		WHERE cs.chain_id = $1 AND (cs.run_id IS NULL OR r.state <> 'success')
		ORDER BY cs.seq LIMIT 1`, chainID).
		Scan(&st.id, &st.seq, &st.operation, &buf, &st.runID, &st.runState)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return driveStep{}, driveComplete, nil
	case err != nil:
		return driveStep{}, driveStopped, fmt.Errorf("next step: %w", err)
	}
	if err := json.Unmarshal(buf, &st.params); err != nil {
		return driveStep{}, driveStopped, fmt.Errorf("step params: %w", err)
	}
	return st, driveAdvance, nil
}

// linkStep points a step at its (latest) run attempt. Resume overwrites the
// failed attempt's id here — the superseded run keeps its run + audit rows,
// it just leaves the strip (mini-ADR 2).
func (s *Service) linkStep(ctx context.Context, stepID, runID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chain_step SET run_id = $2 WHERE id = $1`, stepID, runID)
	return err
}

// halt moves a running chain to halted and notifies, exactly once: the
// guarded UPDATE admits one halter (driver vs boot sweep), and only the
// winner reaches the mail (mini-ADR 7).
func (s *Service) halt(ctx context.Context, chainID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE chain SET state = 'halted', halted_at = now(), updated_at = now()
		WHERE id = $1 AND state = 'running'`, chainID)
	if err != nil {
		return fmt.Errorf("chain: halt: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil // already halted or finished; that outcome stands
	}
	s.notifyHalted(chainID)
	return nil
}

func (s *Service) haltLogged(ctx context.Context, chainID int64) {
	if err := s.halt(ctx, chainID); err != nil {
		s.log.Error("chain halt failed", "chain", chainID, "err", err.Error())
	}
}

// finishLogged moves a running chain to success. Guarded like halt; no
// notification — mail is for endings that need a human (SPEC-014 posture).
func (s *Service) finishLogged(ctx context.Context, chainID int64) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE chain SET state = 'success', finished_at = now(), updated_at = now()
		WHERE id = $1 AND state = 'running'`, chainID); err != nil {
		s.log.Error("chain finish failed", "chain", chainID, "err", err.Error())
	}
}

// notifyHalted mails the DBA list about a halt (D7 content rule), fired
// only after the guarded transition committed, in a tracked goroutine so a
// slow SMTP host never blocks the driver or the boot sweep.
func (s *Service) notifyHalted(chainID int64) {
	if s.Notifier == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx := context.Background() // outlives the halting caller
		c, err := s.Get(ctx, chainID)
		if err != nil {
			s.log.Error("chain notify: load chain", "chain", chainID, "err", err.Error())
			return
		}
		if err := s.Notifier.ChainHalted(ctx, c); err != nil {
			s.log.Error("chain halt notification failed", "chain", chainID, "err", err.Error())
		}
	}()
}
