package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/schedule"
)

// ScheduleService is what the schedule endpoints need from
// internal/schedule; satisfied by *schedule.Service, stubbed in tests.
type ScheduleService interface {
	Create(ctx context.Context, req schedule.CreateRequest) (schedule.Schedule, error)
	List(ctx context.Context) ([]schedule.Schedule, error)
	SetEnabled(ctx context.Context, id int64, enabled bool) (schedule.Schedule, error)
	Delete(ctx context.Context, id int64) error
}

type createScheduleRequest struct {
	Instance  string `json:"instance"`
	Operation string `json:"operation"`
	CronSpec  string `json:"cron_spec"`
	Reason    string `json:"reason"`
	Confirm   string `json:"confirm"`
}

// createSchedule answers POST /api/schedules (dba-only via requireRole).
// Same body cap and reason policy as POST /api/runs; the creator becomes
// the owner every future fire is attributed to (SPEC-022 mini-ADR 9).
func createSchedule(log *slog.Logger, ss ScheduleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, startRunBodyCap)
		var req createScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, "bad JSON body")
			return
		}
		if len(req.Reason) > reasonMaxLen {
			writeJSONError(w, http.StatusBadRequest,
				fmt.Sprintf("reason too long (max %d characters)", reasonMaxLen))
			return
		}
		sc, err := ss.Create(r.Context(), schedule.CreateRequest{
			Instance:  req.Instance,
			Operation: req.Operation,
			CronSpec:  req.CronSpec,
			Reason:    req.Reason,
			Confirm:   req.Confirm,
			CreatedBy: actor,
		})
		switch {
		case errors.Is(err, schedule.ErrBadSpec):
			// The parser's message is the useful part — it reaches the drawer.
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		case errors.Is(err, runs.ErrUnknownOperation):
			writeJSONError(w, http.StatusBadRequest, "unknown operation")
			return
		case errors.Is(err, runs.ErrUnknownInstance):
			writeJSONError(w, http.StatusNotFound, "no such instance")
			return
		case errors.Is(err, runs.ErrProdUnconfirmed):
			writeJSONError(w, http.StatusBadRequest, "prod launch requires typing the instance name")
			return
		case err != nil:
			log.Error("create schedule", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not create schedule")
			return
		}
		writeJSON(w, http.StatusCreated, sc)
	}
}

// listSchedules answers GET /api/schedules (session-gated, like every read).
func listSchedules(log *slog.Logger, ss ScheduleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := ss.List(r.Context())
		if err != nil {
			log.Error("list schedules", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "schedules unavailable")
			return
		}
		if list == nil {
			list = []schedule.Schedule{} // "schedules": [], never null
		}
		writeJSON(w, http.StatusOK, map[string]any{"schedules": list})
	}
}

// patchSchedule answers PATCH /api/schedules/{id} — enable/disable only;
// spec or reason changes are delete + recreate in MVP (SPEC-022).
func patchSchedule(log *slog.Logger, ss ScheduleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := scheduleIDParam(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, "bad JSON body")
			return
		}
		if req.Enabled == nil {
			writeJSONError(w, http.StatusBadRequest, "missing field: enabled")
			return
		}
		sc, err := ss.SetEnabled(r.Context(), id, *req.Enabled)
		switch {
		case errors.Is(err, schedule.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no such schedule")
			return
		case err != nil:
			log.Error("toggle schedule", "schedule", id, "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not update schedule")
			return
		}
		writeJSON(w, http.StatusOK, sc)
	}
}

// deleteSchedule answers DELETE /api/schedules/{id}.
func deleteSchedule(log *slog.Logger, ss ScheduleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := scheduleIDParam(w, r)
		if !ok {
			return
		}
		err := ss.Delete(r.Context(), id)
		switch {
		case errors.Is(err, schedule.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no such schedule")
			return
		case err != nil:
			log.Error("delete schedule", "schedule", id, "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not delete schedule")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// scheduleIDParam parses the {id} route param; non-integers are 404s (ids
// are opaque to clients — same posture as runIDParam).
func scheduleIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "no such schedule")
		return 0, false
	}
	return id, true
}
