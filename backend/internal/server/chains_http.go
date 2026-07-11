package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ios9000/db-portal/backend/internal/chain"
)

// ChainService is what the chain endpoints need from internal/chain;
// satisfied by *chain.Service, stubbed in tests. Chains have no client-
// facing create — they are portal-assembled (SPEC-032; 031's restore POST
// is the first assembler).
type ChainService interface {
	ForRun(ctx context.Context, runID int64) (chain.Chain, error)
	Resume(ctx context.Context, id int64, actor string) (chain.Chain, error)
}

// getRunChain answers GET /api/runs/{id}/chain (session-gated read): the
// chain this run is a step of, for the RunDetail strip. Unknown run and
// not-a-step answer the same 404 — either way there is no strip to show.
func getRunChain(log *slog.Logger, cs ChainService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := runIDParam(w, r)
		if !ok {
			return
		}
		c, err := cs.ForRun(r.Context(), id)
		switch {
		case errors.Is(err, chain.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "run is not part of a chain")
			return
		case err != nil:
			log.Error("run chain lookup", "run", id, "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "chain unavailable")
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

// resumeChain answers POST /api/chains/{id}/resume (dba-only via
// requireRole, no body). The session identity becomes the mover: re-fired
// step runs audit as chain:<resumer> (SPEC-032 mini-ADR 1). 409 when the
// chain isn't halted — the guarded flip makes double-resume single-flight.
func resumeChain(log *slog.Logger, cs ChainService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(w, r)
		if !ok {
			return
		}
		id, ok := chainIDParam(w, r)
		if !ok {
			return
		}
		c, err := cs.Resume(r.Context(), id, actor)
		switch {
		case errors.Is(err, chain.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no such chain")
			return
		case errors.Is(err, chain.ErrNotResumable):
			writeJSONError(w, http.StatusConflict, "chain is not halted")
			return
		case err != nil:
			log.Error("resume chain", "chain", id, "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not resume chain")
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

// chainIDParam parses the {id} route param; non-integers are 404s (ids are
// opaque to clients — same posture as runIDParam).
func chainIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "no such chain")
		return 0, false
	}
	return id, true
}
