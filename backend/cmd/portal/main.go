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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ios9000/db-portal/backend/internal/authn"
	"github.com/ios9000/db-portal/backend/internal/authz"
	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/config"
	"github.com/ios9000/db-portal/backend/internal/db"
	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/notify"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/schedule"
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
	// per env class, never shared (guardrail layer 3). Prod stays MockEngine
	// in dev; the non-prod class is opt-in (SPEC-033 mini-ADR 6) — mock by
	// default, real Semaphore when PORTAL_ENGINE_NONPROD=semaphore. Disjoint
	// config per class; an unknown value fails closed here, before serving.
	nonprod, err := nonProdAdapter(cfg, log)
	if err != nil {
		return err
	}
	registry := engine.NewRegistry()
	registry.Register(engine.ClassProd, engine.NewMockEngine(engine.MockConfig{Name: "mock-prod"}))
	registry.Register(engine.ClassNonProd, nonprod)

	runSvc := runs.NewService(pool, registry, log)
	chainSvc := chain.New(pool, runSvc, log)
	// Failure/cancel mail to the DBA list (SPEC-014) — wired before the
	// orphan sweeps so unattended endings notify too. Chain step runs mail
	// at chain granularity only: the halt mail is THE mail, the run-level
	// one is filtered (SPEC-032 mini-ADR 5). No recipients = notifications
	// off, stated once so nobody hunts for missing mail.
	if to := cfg.NotifyRecipients(); len(to) > 0 {
		mailer := &notify.Mailer{
			Addr: cfg.SMTPAddr(), From: cfg.SMTPFrom, To: to, BaseURL: cfg.BaseURL,
		}
		runSvc.Notifier = chain.StepRunFilter{Next: mailer}
		chainSvc.Notifier = mailer
		log.Info("run notifications enabled", "smtp", cfg.SMTPAddr(), "to", to)
	} else {
		log.Info("run notifications disabled (PORTAL_NOTIFY_TO is empty)")
	}
	// Finalize runs orphaned by a previous process (SPEC-012). A down DB
	// must not stop the server (degraded mode) — warn and continue.
	if n, err := runSvc.SweepOrphans(ctx); err != nil {
		log.Warn("orphan sweep skipped", "err", err.Error())
	} else if n > 0 {
		log.Info("orphaned runs finalized", "count", n)
	}
	// Then halt chains those orphans belonged to (SPEC-032 behavior 6) —
	// this order lets the chain sweep see its step runs already terminal.
	if n, err := chainSvc.SweepOrphans(ctx); err != nil {
		log.Warn("chain sweep skipped", "err", err.Error())
	} else if n > 0 {
		log.Info("orphaned chains halted", "count", n)
	}

	auth, err := buildAuthenticator(cfg, pool, log)
	if err != nil {
		return err
	}
	roles := authz.NewStore(pool, log)
	// Role provisioning follows the auth mode (SPEC-021 mini-ADR 8): dev
	// principals get the dba role at boot; ldap mode grants nothing — real
	// grants are admin INSERTs. Degraded mode (down DB) warns and continues,
	// matching the sweep above; guards then fail closed until the DB is back.
	if devUsers := devGrants(cfg.AuthMode); len(devUsers) > 0 {
		if err := roles.Grant(ctx, authz.RoleDBA, devUsers...); err != nil {
			log.Warn("dev role grants skipped", "err", err.Error())
		}
	}

	// The scheduler executor (ADR-003, SPEC-022) fires due schedules
	// through runSvc.Start — the identical guardrail + audit path as the
	// launch button. Started after the orphan sweep so a misfire catch-up
	// never races the repair of its own half-fired predecessor.
	sched := schedule.New(pool, runSvc, log)
	go sched.Run(ctx)

	log.Info("starting portal", "version", version.Version, "addr", cfg.HTTPAddr)
	return server.New(cfg.HTTPAddr, log, server.Deps{
		DB:            pool,
		Instances:     inventory.NewStore(pool),
		Runs:          runSvc,
		Artifacts:     runSvc,
		Schedules:     sched,
		Chains:        chainSvc,
		Auth:          auth,
		Roles:         roles,
		SecureCookies: cfg.CookieSecure,
	}).Run(ctx)
}

// devGrants names the principals each non-ldap auth mode must be able to
// act as, so dev and demo work out of the box.
// nonProdAdapter builds the non-prod engine class adapter from config
// (SPEC-033 mini-ADR 6): mock by default, real Semaphore when opt-in.
// A disjoint SemaphoreConfig keeps prod/nonprod credentials apart (guardrail
// 3); an unknown mode or a malformed template map fails closed.
func nonProdAdapter(cfg config.Config, log *slog.Logger) (engine.Adapter, error) {
	switch cfg.EngineNonProd {
	case "mock":
		return engine.NewMockEngine(engine.MockConfig{Name: "mock-nonprod"}), nil
	case "semaphore":
		templates, err := cfg.SemaphoreTemplateMap()
		if err != nil {
			return nil, err
		}
		log.Info("non-prod engine: Semaphore",
			"url", cfg.SemaphoreURL, "project", cfg.SemaphoreProjectID, "templates", templates)
		return engine.NewSemaphoreAdapter(engine.SemaphoreConfig{
			BaseURL:      cfg.SemaphoreURL,
			APIToken:     cfg.SemaphoreAPIToken,
			ProjectID:    cfg.SemaphoreProjectID,
			Templates:    templates,
			PollInterval: cfg.SemaphorePollInterval,
		}, log), nil
	default:
		return nil, fmt.Errorf("PORTAL_ENGINE_NONPROD=%q: want %q or %q", cfg.EngineNonProd, "mock", "semaphore")
	}
}

func devGrants(mode string) []string {
	switch mode {
	case "fake":
		return []string{"dba1", "dba2"}
	case "off":
		return []string{"local-dev"}
	}
	return nil
}

// buildAuthenticator is the composition root for SPEC-020's directory seam.
// Unknown modes fail at startup rather than guessing — auth config is not
// a place for silent fallbacks.
func buildAuthenticator(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) (server.Authenticator, error) {
	switch cfg.AuthMode {
	case "off":
		log.Warn("AUTH BYPASS ACTIVE (PORTAL_AUTH_MODE=off) — every request is local-dev; demo/dev only")
		return authn.Bypass{}, nil
	case "fake":
		log.Info("auth mode: fake in-process directory (dev/CI)")
		return authn.NewService(pool, authn.DevDirectory(), log, cfg.SessionTTL, cfg.BreakglassHash), nil
	case "ldap":
		if cfg.LDAPInsecure {
			log.Warn("PORTAL_LDAP_INSECURE=true — LDAP TLS verification is off; dev only")
		}
		// Same loudness as the LDAP warning above (M2-gate finding 10): a
		// prod-shaped boot without the Secure cookie flag must not be silent.
		if !cfg.CookieSecure {
			log.Warn("PORTAL_COOKIE_SECURE=false with ldap auth — the session cookie may travel over plain HTTP; set it true behind TLS")
		}
		dir := authn.LDAP{
			URL:          cfg.LDAPURL,
			BindTemplate: cfg.LDAPBindTemplate,
			Insecure:     cfg.LDAPInsecure,
		}
		log.Info("auth mode: ldap", "url", cfg.LDAPURL)
		return authn.NewService(pool, dir, log, cfg.SessionTTL, cfg.BreakglassHash), nil
	default:
		return nil, fmt.Errorf("unknown PORTAL_AUTH_MODE %q (want ldap, fake or off)", cfg.AuthMode)
	}
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
