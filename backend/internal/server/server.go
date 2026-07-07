// Package server is the portal's HTTP chassis: chi router, request
// logging, health checks, graceful shutdown. Feature WUs mount routes here.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ios9000/db-portal/backend/internal/webui"
)

// Pinger reports database liveness; satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the router's feature dependencies. Production wires the real
// implementations in main; tests substitute per-seam stubs.
type Deps struct {
	DB        Pinger
	Instances InstanceReader
	Runs      RunService
}

// NewRouter builds the portal's HTTP handler with the full middleware
// stack. Kept separate from Server so handler tests exercise exactly
// what production serves.
func NewRouter(log *slog.Logger, d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(requestLogger(log))
	r.Get("/healthz", healthz(d.DB))
	r.Route("/api", func(r chi.Router) {
		r.Get("/instances", listInstances(log, d.Instances))
		r.Get("/instances/{name}", getInstance(log, d.Instances))
		r.Get("/operations", listOperations())
		r.Post("/runs", startRun(log, d.Runs))
		r.Get("/runs", listRuns(log, d.Runs))
		r.Get("/runs/{id}", getRun(log, d.Runs))
		r.Get("/runs/{id}/logs", streamRunLogs(log, d.Runs))
		r.Post("/runs/{id}/cancel", cancelRun(log, d.Runs))
	})
	// Everything unmatched goes to the embedded SPA (WU-006): real files
	// as-is, client-side routes fall back to index.html, /api misses stay 404.
	r.NotFound(webui.Handler().ServeHTTP)
	return r
}

// Server wraps http.Server with graceful shutdown.
type Server struct {
	http *http.Server
	log  *slog.Logger
}

func New(addr string, log *slog.Logger, d Deps) *Server {
	return &Server{
		http: &http.Server{
			Addr:              addr,
			Handler:           NewRouter(log, d),
			ReadHeaderTimeout: 5 * time.Second,
		},
		log: log,
	}
}

// Run serves until ctx is cancelled (SIGINT/SIGTERM in main), then shuts
// down gracefully, giving in-flight requests up to 10s to finish.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err // ListenAndServe never returns nil
	case <-ctx.Done():
		s.log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, s.http.Close())
		}
		return nil
	}
}
