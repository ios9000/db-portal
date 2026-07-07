package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
}

// Store reads the inventory tables. Writes happen only through Import.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const instanceColumns = `
	SELECT i.name, c.name, i.env, c.platform, i.pg_version, i.size_gb, i.owner, i.maintenance_window
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
			&in.PGVersion, &in.SizeGB, &in.Owner, &in.MaintenanceWindow); err != nil {
			return nil, fmt.Errorf("inventory: scan instance: %w", err)
		}
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
			&in.PGVersion, &in.SizeGB, &in.Owner, &in.MaintenanceWindow)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Instance{}, ErrNotFound
	case err != nil:
		return Instance{}, fmt.Errorf("inventory: get instance %q: %w", name, err)
	}
	return in, nil
}
