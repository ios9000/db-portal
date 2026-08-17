package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/window"
)

// ErrNotFound is returned by GetInstance for an unknown instance name.
var ErrNotFound = errors.New("inventory: instance not found")

// Instance is the read model served by the API: an instance row joined
// with its cluster. Nullable columns come through as nil pointers.
type Instance struct {
	Name              string   `json:"name"`
	Cluster           string   `json:"cluster"`
	Env               string   `json:"env"`
	Platform          string   `json:"platform"`
	PGVersion         string   `json:"pg_version"`
	SizeGB            *float64 `json:"size_gb"`
	Owner             string   `json:"owner"`
	MaintenanceWindow *string  `json:"maintenance_window"`
	// LastBackupAt is when the newest successful dump run on this instance
	// finished (WU-011R); nil until one exists. Only backups the portal ran
	// count — it has no visibility into backups taken elsewhere.
	LastBackupAt *time.Time `json:"last_backup_at"`
	// WindowState says where "now" sits relative to the maintenance window
	// (SPEC-023 mini-ADR 5): "inside" | "outside", nil when the instance
	// has no window or the text doesn't parse. Server-computed at read
	// time — the frontend displays it and never parses window text.
	WindowState *string `json:"window_state"`
}

// windowState evaluates the window against the server clock; every parse
// failure is nil (D6: windows warn, never block — and never error).
func windowState(raw *string) *string {
	if raw == nil || *raw == "" {
		return nil
	}
	w, err := window.Parse(*raw)
	if err != nil {
		return nil
	}
	state := "outside"
	if w.Contains(time.Now()) {
		state = "inside"
	}
	return &state
}

// Store reads the inventory tables. Writes happen only through Import.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const instanceColumns = `
	SELECT i.name, c.name, i.env, c.platform, i.pg_version, i.size_gb, i.owner, i.maintenance_window,
	       (SELECT max(r.finished_at) FROM run r
	        WHERE r.instance_id = i.id AND r.operation = 'dump' AND r.state = 'success')
	FROM instance i
	JOIN cluster c ON c.id = i.cluster_id`

// ListInstances returns instances ordered by name. env narrows to one
// environment; "" returns everything. The caller validates env — an
// unknown value simply matches nothing here.
func (s *Store) ListInstances(ctx context.Context, env string) ([]Instance, error) {
	query, args := instanceColumns+` ORDER BY i.name`, []any{}
	if env != "" {
		query, args = instanceColumns+` WHERE i.env = $1 ORDER BY i.name`, []any{env}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("inventory: list instances: %w", err)
	}
	defer rows.Close()

	instances := []Instance{}
	for rows.Next() {
		var in Instance
		if err := rows.Scan(&in.Name, &in.Cluster, &in.Env, &in.Platform,
			&in.PGVersion, &in.SizeGB, &in.Owner, &in.MaintenanceWindow, &in.LastBackupAt); err != nil {
			return nil, fmt.Errorf("inventory: scan instance: %w", err)
		}
		in.WindowState = windowState(in.MaintenanceWindow)
		instances = append(instances, in)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inventory: list instances: %w", err)
	}
	return instances, nil
}

// GetInstance returns the instance with the given name (the natural key),
// or ErrNotFound.
func (s *Store) GetInstance(ctx context.Context, name string) (Instance, error) {
	var in Instance
	err := s.pool.QueryRow(ctx, instanceColumns+` WHERE i.name = $1`, name).
		Scan(&in.Name, &in.Cluster, &in.Env, &in.Platform,
			&in.PGVersion, &in.SizeGB, &in.Owner, &in.MaintenanceWindow, &in.LastBackupAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Instance{}, ErrNotFound
	case err != nil:
		return Instance{}, fmt.Errorf("inventory: get instance %q: %w", name, err)
	}
	in.WindowState = windowState(in.MaintenanceWindow)
	return in, nil
}

// InventoryHost implements engine.InventorySource (SPEC-050 mini-ADR 5): the
// connection facts the local engine renders into a per-job inventory, read
// FRESH at StartJob so a job targets the fleet model as imported now.
// Addresses and facts only, never credentials. An instance with no recorded
// tuple comes back with Host "" — the engine fails closed on it.
func (s *Store) InventoryHost(ctx context.Context, name string) (engine.InventoryHost, error) {
	var (
		h    engine.InventoryHost
		host *string
		port *int
	)
	err := s.pool.QueryRow(ctx, `
		SELECT i.name, i.host, i.port, i.env, c.platform, c.name
		FROM instance i JOIN cluster c ON c.id = i.cluster_id
		WHERE i.name = $1`, name).
		Scan(&h.Name, &host, &port, &h.Env, &h.Platform, &h.Cluster)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return engine.InventoryHost{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	case err != nil:
		return engine.InventoryHost{}, fmt.Errorf("inventory: connection info for %q: %w", name, err)
	}
	if host != nil {
		h.Host = *host
	}
	if port != nil {
		h.Port = *port
	}
	return h, nil
}
