// Command portal is the DB Portal server binary.
//
//	portal                       serve the API (and, from WU-006, the SPA)
//	portal migrate up|down|status  run embedded goose migrations
//	portal version               print the build version
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ios9000/db-portal/backend/internal/config"
	"github.com/ios9000/db-portal/backend/internal/db"
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
	if len(args) > 0 {
		return fmt.Errorf("unknown command %q (want migrate or version)", args[0])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("starting portal", "version", version.Version, "addr", cfg.HTTPAddr)
	return server.New(cfg.HTTPAddr, log, pool).Run(ctx)
}
