package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ios9000/db-portal/backend/internal/catalog"
	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

// RunService is what the run endpoints need from internal/runs; satisfied
// by *runs.Service, stubbed in handler tests.
type RunService interface {
	Start(ctx context.Context, instanceName, operationID, reason string, engineParams map[string]string) (runs.Run, error)
	Get(ctx context.Context, id int64) (runs.Run, error)
	List(ctx context.Context, instanceName string) ([]runs.Run, error)
	StreamLogs(ctx context.Context, id int64) (<-chan engine.LogLine, error)
	Cancel(ctx context.Context, id int64) error
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

// runIDParam parses the {id} route param; any non-integer is a 404 (ids
// are opaque to clients, so a malformed one is just an unknown run).
func runIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "no such run")
		return 0, false
	}
	return id, true
}

// sseLogLine is the `log` event payload; time.Time marshals RFC 3339 Nano.
type sseLogLine struct {
	TS   time.Time `json:"ts"`
	Line string    `json:"line"`
}

// writeSSE emits one Server-Sent Event and flushes it to the client.
func writeSSE(w http.ResponseWriter, fl http.Flusher, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte("{}")
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	fl.Flush()
}

// streamRunLogs answers GET /api/runs/{id}/logs with an SSE stream
// (SPEC-013): `log` events replaying then following the engine job's
// output, then exactly one `end` event so EventSource clients know to
// close instead of auto-reconnecting into an endless replay loop.
func streamRunLogs(log *slog.Logger, rs RunService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := runIDParam(w, r)
		if !ok {
			return
		}
		ch, err := rs.StreamLogs(r.Context(), id)
		switch {
		case errors.Is(err, runs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no such run")
			return
		case errors.Is(err, runs.ErrNoLogs):
			// Logs die with the engine job (SPEC-013 mini-ADR 3); the run
			// row still serves the outcome.
			writeJSONError(w, http.StatusGone, "logs no longer available")
			return
		case err != nil:
			log.Error("stream logs", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "logs unavailable")
			return
		}

		fl, ok := w.(http.Flusher)
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // reverse proxies must not buffer the stream
		w.WriteHeader(http.StatusOK)
		fl.Flush()

		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		for {
			select {
			case l, open := <-ch:
				if !open {
					// The end state can lag the engine by one watcher poll;
					// clients close the stream and re-fetch the run anyway.
					state := ""
					if run, err := rs.Get(r.Context(), id); err == nil {
						state = run.State
					}
					writeSSE(w, fl, "end", map[string]string{"state": state})
					return
				}
				writeSSE(w, fl, "log", sseLogLine{TS: l.TS, Line: l.Line})
			case <-keepalive.C:
				_, _ = fmt.Fprint(w, ": keepalive\n\n")
				fl.Flush()
			case <-r.Context().Done():
				return // client left; ctx cancel detaches the engine sub
			}
		}
	}
}

// cancelRun answers POST /api/runs/{id}/cancel. Cancel is asynchronous:
// 202 means the engine took the request; the watcher finalizes the run
// (SPEC-013 mini-ADR 4) and clients observe `canceled` via polling.
func cancelRun(log *slog.Logger, rs RunService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := runIDParam(w, r)
		if !ok {
			return
		}
		err := rs.Cancel(r.Context(), id)
		switch {
		case errors.Is(err, runs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no such run")
		case errors.Is(err, runs.ErrNotCancelable):
			writeJSONError(w, http.StatusConflict, "run is not cancelable")
		case errors.Is(err, runs.ErrEngine):
			log.Error("cancel run", "run", id, "err", err.Error())
			writeJSONError(w, http.StatusBadGateway, "engine refused the cancel")
		case err != nil:
			log.Error("cancel run", "run", id, "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not cancel run")
		default:
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "canceling"})
		}
	}
}

// getRun answers GET /api/runs/{id}.
func getRun(log *slog.Logger, rs RunService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := runIDParam(w, r)
		if !ok {
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
