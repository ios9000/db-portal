package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ios9000/db-portal/backend/internal/runs"
)

// ArtifactReader is what the artifact + restore endpoints need from the
// registry; satisfied by *runs.Service, stubbed in handler tests.
type ArtifactReader interface {
	ListArtifacts(ctx context.Context, instance string) ([]runs.RegisteredArtifact, error)
	GetArtifact(ctx context.Context, id int64) (runs.RegisteredArtifact, error)
}

// listArtifacts answers GET /api/artifacts?instance=<name> (SPEC-030).
// The param is required — the registry read is the restore drawer's
// per-instance feed, not a global browse (mini-ADR 4) — and an unknown
// instance is a 404, matching GET /api/instances/{name}.
func listArtifacts(log *slog.Logger, ar ArtifactReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instance := r.URL.Query().Get("instance")
		if instance == "" {
			writeJSONError(w, http.StatusBadRequest, "instance query parameter is required")
			return
		}
		artifacts, err := ar.ListArtifacts(r.Context(), instance)
		switch {
		case errors.Is(err, runs.ErrUnknownInstance):
			writeJSONError(w, http.StatusNotFound, "no such instance")
			return
		case err != nil:
			log.Error("list artifacts", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "artifacts unavailable")
			return
		}
		if artifacts == nil {
			artifacts = []runs.RegisteredArtifact{} // "artifacts": [], never null
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
	}
}
