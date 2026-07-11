package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/restore"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

type restoreRequest struct {
	ArtifactID int64  `json:"artifact_id"`
	Target     string `json:"target"`
	Confirm    string `json:"confirm"`
	Reason     string `json:"reason"`
}

// startRestore answers POST /api/restore — the first (and MVP-only) chain
// assembler (SPEC-031 mini-ADR 4). It loads the source artifact from the
// registry, builds the fixed verify → safety_dump → restore recipe (the
// safety dump is unconditional — restore.Steps always includes it), and hands
// it to chain.Service.Create, which enforces the target's prod ritual before
// any row is written. Body-capped and reason-length-capped like every mutation.
func startRestore(log *slog.Logger, ar ArtifactReader, cs ChainService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, startRunBodyCap)
		var req restoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, "bad JSON body")
			return
		}
		if req.ArtifactID <= 0 {
			writeJSONError(w, http.StatusBadRequest, "artifact_id is required")
			return
		}
		if req.Target == "" {
			writeJSONError(w, http.StatusBadRequest, "target is required")
			return
		}
		if len(req.Reason) > reasonMaxLen {
			writeJSONError(w, http.StatusBadRequest,
				fmt.Sprintf("reason too long (max %d characters)", reasonMaxLen))
			return
		}

		art, err := ar.GetArtifact(r.Context(), req.ArtifactID)
		switch {
		case errors.Is(err, runs.ErrArtifactNotFound):
			writeJSONError(w, http.StatusNotFound, "no such artifact")
			return
		case err != nil:
			log.Error("restore: load artifact", "artifact", req.ArtifactID, "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not start restore")
			return
		}

		c, err := cs.Create(r.Context(), chain.CreateRequest{
			Kind:     restore.Kind,
			Instance: req.Target,
			Actor:    actor,
			Confirm:  req.Confirm,
			Reason:   req.Reason,
			Steps:    restore.Steps(art.ID, art.Checksum, art.Name),
		})
		switch {
		case errors.Is(err, runs.ErrUnknownInstance):
			writeJSONError(w, http.StatusNotFound, "no such instance")
			return
		case errors.Is(err, runs.ErrProdUnconfirmed):
			writeJSONError(w, http.StatusBadRequest, "prod restore requires typing the target instance name")
			return
		case err != nil:
			// ErrUnknownOperation here means our own recipe drifted from the
			// catalog — an internal fault, not a client one.
			log.Error("restore: assemble chain", "target", req.Target, "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "could not start restore")
			return
		}
		writeJSON(w, http.StatusCreated, c)
	}
}
