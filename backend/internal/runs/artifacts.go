package runs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RegisteredArtifact is the API read model of one artifact-registry row
// (SPEC-030) — what can be restored, keyed to its origin run. Distinct from
// Artifact, the run read model's "what this run produced" sub-object.
// location is deliberately absent until it means something (WU-034).
type RegisteredArtifact struct {
	ID             int64     `json:"id"`
	RunID          int64     `json:"run_id"`
	Name           string    `json:"name"`
	SizeBytes      int64     `json:"size_bytes"`
	Checksum       string    `json:"checksum"`
	RetentionClass string    `json:"retention_class"`
	CreatedAt      time.Time `json:"created_at"`
}

// ListArtifacts returns the instance's newest 50 registry rows (SPEC-030
// mini-ADR 4: the restore drawer's per-instance feed — an unknown instance
// is ErrUnknownInstance, not an empty list).
func (s *Service) ListArtifacts(ctx context.Context, instance string) ([]RegisteredArtifact, error) {
	var instanceID int64
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM instance WHERE name = $1`, instance).Scan(&instanceID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("%w: %q", ErrUnknownInstance, instance)
	case err != nil:
		return nil, fmt.Errorf("runs: look up instance: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.run_id, a.name, a.size_bytes, a.checksum, a.retention_class, a.created_at
		FROM artifact a
		JOIN run r ON r.id = a.run_id
		WHERE r.instance_id = $1
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT 50`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("runs: list artifacts: %w", err)
	}
	defer rows.Close()

	out := []RegisteredArtifact{}
	for rows.Next() {
		var a RegisteredArtifact
		if err := rows.Scan(&a.ID, &a.RunID, &a.Name, &a.SizeBytes,
			&a.Checksum, &a.RetentionClass, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("runs: scan artifact: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: list artifacts: %w", err)
	}
	return out, nil
}
