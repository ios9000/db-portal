package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
)

// InstanceReader is what the instance endpoints need from the inventory;
// satisfied by *inventory.Store. Handler tests stub it so they run without
// a database.
type InstanceReader interface {
	ListInstances(ctx context.Context, env string) ([]inventory.Instance, error)
	GetInstance(ctx context.Context, name string) (inventory.Instance, error)
}

// listInstances answers GET /api/instances[?env=dev|test|prod].
func listInstances(log *slog.Logger, inv InstanceReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		env := r.URL.Query().Get("env")
		if env != "" {
			// Same authority as import and guardrails: an env the engine
			// cannot classify does not exist.
			if _, err := engine.ClassForEnv(env); err != nil {
				writeJSONError(w, http.StatusBadRequest, "unknown env (want dev|test|prod)")
				return
			}
		}
		instances, err := inv.ListInstances(r.Context(), env)
		if err != nil {
			log.Error("list instances", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "inventory unavailable")
			return
		}
		if instances == nil {
			instances = []inventory.Instance{} // "instances": [], never null
		}
		writeJSON(w, http.StatusOK, map[string]any{"instances": instances})
	}
}

// getInstance answers GET /api/instances/{name}.
func getInstance(log *slog.Logger, inv InstanceReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instance, err := inv.GetInstance(r.Context(), chi.URLParam(r, "name"))
		switch {
		case errors.Is(err, inventory.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no such instance")
			return
		case err != nil:
			log.Error("get instance", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "inventory unavailable")
			return
		}
		writeJSON(w, http.StatusOK, instance)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
