package server

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
)

// Reconciler is what the Semaphore webhook needs from internal/runs: map a
// Semaphore task id to its run and re-poll to (maybe) finalize it. Satisfied
// by *runs.Service.ReconcileByJobID; stubbed in handler tests.
type Reconciler interface {
	ReconcileByJobID(ctx context.Context, jobID string) error
}

// webhookSecretHeader carries the shared secret. The request logger records
// only method/path/status (never headers or bodies), so the secret does not
// reach the logs (SPEC-033 mini-ADR 5 + the no-secrets-in-logs ground rule).
const webhookSecretHeader = "X-Portal-Webhook-Secret"

// semaphoreWebhookBodyCap bounds the webhook body. The handler reads exactly
// one small integer (a task id) out of it; nothing else is parsed or stored.
const semaphoreWebhookBodyCap = 64 << 10

// semaphoreWebhookPayload is the ONLY thing the handler reads from the body: a
// task id, tolerated in the shapes Semaphore alerters emit. The claimed status
// is deliberately absent from this struct — the handler re-polls the real task
// (mini-ADR 2), so a payload can name a task but never assert its outcome.
type semaphoreWebhookPayload struct {
	TaskID *int64 `json:"task_id"`
	Task   *struct {
		ID int64 `json:"id"`
	} `json:"task"`
	ID *int64 `json:"id"`
}

// taskID resolves the task id from whichever field carried it, "" if none.
func (p semaphoreWebhookPayload) taskID() string {
	switch {
	case p.TaskID != nil:
		return strconv.FormatInt(*p.TaskID, 10)
	case p.Task != nil:
		return strconv.FormatInt(p.Task.ID, 10)
	case p.ID != nil:
		return strconv.FormatInt(*p.ID, 10)
	default:
		return ""
	}
}

// webhookSemaphore answers POST /api/engine/semaphore/webhook — the
// session-less accelerator for Semaphore terminal-status (SPEC-033 mini-ADR
// 5). It ACCELERATES finalization; it is never the source of truth. The
// handler:
//   - authenticates a shared secret in CONSTANT time (hmac.Equal). An empty
//     configured secret disables the accelerator: the route exists but 401s
//     everything (poll-only mode, the slice-(a) posture). A missing/wrong
//     secret → 401 + ZERO state change (auth precedes any body read).
//   - reads NOTHING but the task id from the body, then triggers ONE re-poll
//     of the REAL task via ReconcileByJobID. The payload's claimed status is
//     ignored — Semaphore's live status finalizes, so a spoofed body that
//     clears the secret bar still can't force a wrong outcome (defense in
//     depth), and there is no write path from the body (no injection surface).
//
// A reconcile error is logged, never surfaced: a non-2xx would make Semaphore
// retry, and the periodic watcher finalizes regardless (poll is the truth).
func webhookSemaphore(log *slog.Logger, secret string, rec Reconciler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth first, before reading a single body byte — a rejected request
		// must not cause any work or state change.
		presented := r.Header.Get(webhookSecretHeader)
		if secret == "" || !hmac.Equal([]byte(presented), []byte(secret)) {
			log.Warn("semaphore webhook rejected",
				"reason", "missing or wrong secret", "remote", r.RemoteAddr)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, semaphoreWebhookBodyCap)
		var p semaphoreWebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, "bad JSON body")
			return
		}
		taskID := p.taskID()
		if taskID == "" {
			writeJSONError(w, http.StatusBadRequest, "no task id in webhook payload")
			return
		}
		if err := rec.ReconcileByJobID(r.Context(), taskID); err != nil {
			log.Error("semaphore webhook reconcile failed", "task", taskID, "err", err.Error())
		}
		// The re-poll ran synchronously above: if the task was terminal the run
		// is already finalized. No body — Semaphore ignores it.
		w.WriteHeader(http.StatusNoContent)
	}
}
