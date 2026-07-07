package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ios9000/db-portal/backend/internal/catalog"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

// RunService is what the run endpoints need from internal/runs; satisfied
// by *runs.Service, stubbed in handler tests.
type RunService interface {
	Start(ctx context.Context, instanceName, operationID, reason string, engineParams map[string]string) (runs.Run, error)
	Get(ctx context.Context, id int64) (runs.Run, error)
	List(ctx context.Context, instanceName string) ([]runs.Run, error)
}

// listOperations answers GET /api/operations from the static catalog.
func listOperations() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"operations": catalog.All()})
	}
}

type startRunRequest struct {
	Instance  string `json:"instance"`
	Operation string `json:"operation"`
	Reason    string `json:"reason"`
}

// startRun answers POST /api/runs — the hero flow's entry point. No engine
// params cross this boundary in MVP (SPEC-012).
func startRun(log *slog.Logger, rs RunService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req startRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad JSON body")
			return
		}
		run, err := rs.Start(r.Context(), req.Instance, req.Operation, req.Reason, nil)
		switch {
		case errors.Is(err, runs.ErrUnknownOperation):
			writeJSONError(w, http.StatusBadRequest, "unknown operation")
			return
		case errors.Is(err, runs.ErrUnknownInstance):
			writeJSONError(w, http.StatusNotFound, "no such instance")
			return
		case errors.Is(err, runs.ErrEngine):
			// The run exists, finalized failed, audit trail complete — the
			// engine just refused it.
			log.Error("engine refused run", "err", err.Error())
			writeJSONError(w, http.StatusBadGateway, "engine refused the job")
			return
		case err != nil:
			log.Error("start run", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not start run")
			return
		}
		writeJSON(w, http.StatusCreated, run)
	}
}

// listRuns answers GET /api/runs[?instance=name].
func listRuns(log *slog.Logger, rs RunService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := rs.List(r.Context(), r.URL.Query().Get("instance"))
		if err != nil {
			log.Error("list runs", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "runs unavailable")
			return
		}
		if list == nil {
			list = []runs.Run{} // "runs": [], never null
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": list})
	}
}

// getRun answers GET /api/runs/{id}.
func getRun(log *slog.Logger, rs RunService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "no such run")
			return
		}
		run, err := rs.Get(r.Context(), id)
		switch {
		case errors.Is(err, runs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no such run")
			return
		case err != nil:
			log.Error("get run", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "runs unavailable")
			return
		}
		writeJSON(w, http.StatusOK, run)
	}
}
