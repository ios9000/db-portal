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

// drive advances one chain to success or halt: fire the current step
// through runs.Start, watch its run to terminal, repeat (SPEC-032 mini-ADR
// 6 — create and resume share this loop; mover is whoever set THIS pass in
// motion, so step runs audit as chain:<creator> or chain:<resumer>).
// Fire-time errors halt visibly instead of surfacing to a caller — the
// scheduler's last_fire_status='error' posture. DB errors exit the loop and
// leave the chain `running` for the boot sweep to repair; there is no safe
// in-process retry that can't also fail.
func (s *Service) drive(chainID int64, mover string) {
	defer s.wg.Done()
	ctx := context.Background() // outlives the creating/resuming request

	var instance, confirm string
	var reason *string
	err := s.pool.QueryRow(ctx, `
		SELECT i.name, c.confirm, c.reason
		FROM chain c JOIN instance i ON i.id = c.instance_id
		WHERE c.id = $1`, chainID).Scan(&instance, &confirm, &reason)
	if err != nil {
		s.log.Error("chain: driver load failed", "chain", chainID, "err", err.Error())
		return
	}

	for {
		step, status, err := s.next(ctx, chainID)
		if err != nil {
			s.log.Error("chain: next step lookup failed", "chain", chainID, "err", err.Error())
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
				Actor:        "chain:" + mover,
				Instance:     instance,
				Operation:    step.operation,
				Reason:       reasonStr,
				Confirm:      confirm,
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

		for {
			run, err := s.runs.Get(ctx, runID)
			if err != nil {
				s.log.Error("chain: step watch failed", "chain", chainID, "run", runID, "err", err.Error())
				return
			}
			if runTerminal(run.State) {
				if run.State != runSuccess {
					s.haltLogged(ctx, chainID)
					return
				}
				break // next step
			}
			time.Sleep(s.PollInterval)
		}
	}
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
