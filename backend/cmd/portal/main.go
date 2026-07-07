// Command portal is the DB Portal server binary.
//
//	portal                       serve the API + embedded SPA
//	portal migrate up|down|status  run embedded goose migrations
//	portal import <file.csv>     import the instance inventory (SPEC-010)
//	portal version               print the build version
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ios9000/db-portal/backend/internal/config"
	"github.com/ios9000/db-portal/backend/internal/db"
	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/server"
	"github.com/ios9000/db-portal/backend/internal/version"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	if err := run(log, os.Args[1:]); err != nil {
		log.Error("fatal", "err", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	if len(args) > 0 && args[0] == "version" {
		fmt.Println("db-portal", version.Version)
		return nil
	}

	cfg, err := config.Load(config.LocateDotenv())
	if err != nil {
		return err
	}

	if len(args) > 0 && args[0] == "migrate" {
		if len(args) < 2 {
			return fmt.Errorf("usage: portal migrate <up|down|status>")
		}
		return db.Migrate(context.Background(), cfg.DSN(), args[1])
	}
	if len(args) > 0 && args[0] == "import" {
		if len(args) != 2 {
			return fmt.Errorf("usage: portal import <file.csv>")
		}
		return runImport(context.Background(), cfg, args[1])
	}
	if len(args) > 0 {
		return fmt.Errorf("unknown command %q (want migrate, import or version)", args[0])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	// Composition root for the engine seam (ADR-002): one adapter instance
	// per env class, never shared (guardrail layer 3). MockEngine until
	// WU-033 wires Semaphore.
	registry := engine.NewRegistry()
	registry.Register(engine.ClassProd, engine.NewMockEngine(engine.MockConfig{Name: "mock-prod"}))
	registry.Register(engine.ClassNonProd, engine.NewMockEngine(engine.MockConfig{Name: "mock-nonprod"}))

	runSvc := runs.NewService(pool, registry, log)
	// Finalize runs orphaned by a previous process (SPEC-012). A down DB
	// must not stop the server (degraded mode) — warn and continue.
	if n, err := runSvc.SweepOrphans(ctx); err != nil {
		log.Warn("orphan sweep skipped", "err", err.Error())
	} else if n > 0 {
		log.Info("orphaned runs finalized", "count", n)
	}

	log.Info("starting portal", "version", version.Version, "addr", cfg.HTTPAddr)
	return server.New(cfg.HTTPAddr, log, server.Deps{
		DB:        pool,
		Instances: inventory.NewStore(pool),
		Runs:      runSvc,
	}).Run(ctx)
}

// runImport implements `portal import <file>`. Exit 0 means the file was
// processed (quarantined rows included — details go to stderr, the one-line
// report to stdout); a returned error means a file-level failure (exit 1,
// nothing written).
func runImport(ctx context.Context, cfg config.Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read-only; read errors surface in Import

	pool, err := db.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	report, err := inventory.Import(ctx, pool, filepath.Base(path), f)
	if err != nil {
		return err
	}
	for _, rej := range report.Rejects {
		fmt.Fprintf(os.Stderr, "quarantined line %d: %s\n", rej.Line, strings.Join(rej.Reasons, "; "))
	}
	fmt.Println(report)
	return nil
}
