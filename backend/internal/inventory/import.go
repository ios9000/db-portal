package inventory

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Report is one import run's outcome. String() is the SPEC-010 report line.
type Report struct {
	Total       int
	New         int
	Updated     int
	Unchanged   int
	Quarantined int
	Rejects     []Reject
}

func (r Report) String() string {
	return fmt.Sprintf("imported %d new, updated %d, unchanged %d, quarantined %d",
		r.New, r.Updated, r.Unchanged, r.Quarantined)
}

// Import parses an inventory CSV and applies it to the database in a single
// transaction, keyed on instance.name (idempotent). File-level failures
// return an error before anything is written; quarantined rows land in
// inventory_import_reject and do not fail the run.
func Import(ctx context.Context, pool *pgxpool.Pool, filename string, r io.Reader) (Report, error) {
	parsed, err := Parse(r)
	if err != nil {
		return Report{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("inventory: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	rep := Report{Total: parsed.Total, Rejects: parsed.Rejects}
	clusters := map[string]int64{} // resolved this run: name -> id

	for _, row := range parsed.Rows {
		clusterID, ok := clusters[row.ClusterName]
		if !ok {
			id, conflict, err := resolveCluster(ctx, tx, row.ClusterName, row.Platform)
			if err != nil {
				return Report{}, err
			}
			if conflict != "" {
				// Platform disagrees with a cluster from an earlier import:
				// quarantine, same as an in-file conflict (behavior 8).
				rep.Rejects = append(rep.Rejects, Reject{Line: row.Line, Raw: row.Raw, Reasons: []string{conflict}})
				continue
			}
			clusters[row.ClusterName] = id
			clusterID = id
		}
		outcome, err := upsertInstance(ctx, tx, clusterID, row)
		if err != nil {
			return Report{}, err
		}
		switch outcome {
		case rowNew:
			rep.New++
		case rowUpdated:
			rep.Updated++
		case rowUnchanged:
			rep.Unchanged++
		}
	}
	rep.Quarantined = len(rep.Rejects)

	var importID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO inventory_import
			(filename, rows_total, rows_new, rows_updated, rows_unchanged, rows_quarantined)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		filename, rep.Total, rep.New, rep.Updated, rep.Unchanged, rep.Quarantined).Scan(&importID)
	if err != nil {
		return Report{}, fmt.Errorf("inventory: record import: %w", err)
	}
	for _, rej := range rep.Rejects {
		if _, err := tx.Exec(ctx, `
			INSERT INTO inventory_import_reject (import_id, row_number, raw, reasons)
			VALUES ($1, $2, $3, $4)`,
			importID, rej.Line, rej.Raw, rej.Reasons); err != nil {
			return Report{}, fmt.Errorf("inventory: record reject: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Report{}, fmt.Errorf("inventory: commit: %w", err)
	}
	return rep, nil
}

// resolveCluster returns the id of the named cluster, creating it on first
// reference. A non-empty conflict string means the cluster already exists
// with a different platform and the caller must quarantine the row.
func resolveCluster(ctx context.Context, tx pgx.Tx, name, platform string) (id int64, conflict string, err error) {
	var existing string
	err = tx.QueryRow(ctx, `SELECT id, platform FROM cluster WHERE name = $1`, name).Scan(&id, &existing)
	switch {
	case err == nil:
		if existing != platform {
			return 0, platformConflict(name, existing), nil
		}
		return id, "", nil
	case errors.Is(err, pgx.ErrNoRows):
		err = tx.QueryRow(ctx, `INSERT INTO cluster (name, platform) VALUES ($1, $2) RETURNING id`,
			name, platform).Scan(&id)
		if err != nil {
			return 0, "", fmt.Errorf("inventory: create cluster %q: %w", name, err)
		}
		return id, "", nil
	default:
		return 0, "", fmt.Errorf("inventory: look up cluster %q: %w", name, err)
	}
}

type rowOutcome int

const (
	rowUnchanged rowOutcome = iota
	rowNew
	rowUpdated
)

// upsertInstance inserts or overwrites the instance named row.InstanceName
// (natural key). Rows whose values already match are reported unchanged and
// not touched, so updated_at only moves on real changes.
func upsertInstance(ctx context.Context, tx pgx.Tx, clusterID int64, row Row) (rowOutcome, error) {
	var (
		curClusterID           int64
		curEnv, curVer, curOwn string
		curSize, curWindow     *string
	)
	err := tx.QueryRow(ctx, `
		SELECT cluster_id, env, pg_version, size_gb::text, owner, maintenance_window
		FROM instance WHERE name = $1`, row.InstanceName).
		Scan(&curClusterID, &curEnv, &curVer, &curSize, &curOwn, &curWindow)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, err = tx.Exec(ctx, `
			INSERT INTO instance (name, cluster_id, env, pg_version, size_gb, owner, maintenance_window)
			VALUES ($1, $2, $3, $4, $5::numeric, $6, $7)`,
			row.InstanceName, clusterID, row.Env, row.PGVersion, row.SizeGB, row.Owner, row.MaintenanceWindow)
		if err != nil {
			return 0, fmt.Errorf("inventory: insert instance %q: %w", row.InstanceName, err)
		}
		return rowNew, nil
	case err != nil:
		return 0, fmt.Errorf("inventory: look up instance %q: %w", row.InstanceName, err)
	}

	if curClusterID == clusterID && curEnv == row.Env && curVer == row.PGVersion &&
		eqPtr(curSize, row.SizeGB) && curOwn == row.Owner && eqPtr(curWindow, row.MaintenanceWindow) {
		return rowUnchanged, nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE instance
		SET cluster_id = $2, env = $3, pg_version = $4, size_gb = $5::numeric,
		    owner = $6, maintenance_window = $7, updated_at = now()
		WHERE name = $1`,
		row.InstanceName, clusterID, row.Env, row.PGVersion, row.SizeGB, row.Owner, row.MaintenanceWindow)
	if err != nil {
		return 0, fmt.Errorf("inventory: update instance %q: %w", row.InstanceName, err)
	}
	return rowUpdated, nil
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
