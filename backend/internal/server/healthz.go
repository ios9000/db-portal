package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// healthz reports service + database health: 200 {"status":"ok","db":"ok"}
// when the DB answers a ping, 503 {"status":"degraded","db":"down"} when not.
func healthz(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		body := map[string]string{"status": "ok", "db": "ok"}
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			body["status"] = "degraded"
			body["db"] = "down"
		}
		_ = json.NewEncoder(w).Encode(body)
	}
}
