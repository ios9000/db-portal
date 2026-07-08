package runs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Run is the API read model of one run.
type Run struct {
	ID          int64      `json:"id"`
	Instance    string     `json:"instance"`
	Environment string     `json:"environment"`
	Operation   string     `json:"operation"`
	State       string     `json:"state"`
	Reason      *string    `json:"reason"`
	Error       *string    `json:"error"`
	JobID       *string    `json:"job_id"`
	RequestedBy string     `json:"requested_by"`
	SubmittedAt time.Time  `json:"submitted_at"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Artifact    *Artifact  `json:"artifact"`
}

// Artifact is dump-output metadata (O-1 mock path: metadata only, no bytes).
type Artifact struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Checksum  string `json:"checksum"`
}

// requested_by surfaces the audit trail's actor on the read model
// (SPEC-014 mini-ADR 6) instead of duplicating it onto the run row.
const runColumns = `
	SELECT r.id, i.name, r.environment, r.operation, r.state, r.reason, r.error,
		r.job_id, r.submitted_at, r.started_at, r.finished_at,
		r.artifact_name, r.artifact_size_bytes, r.artifact_checksum,
		COALESCE((SELECT a.actor FROM audit_event a
			WHERE a.run_id = r.id AND a.action = 'run.submitted'
			ORDER BY a.id LIMIT 1), '') AS requested_by
	FROM run r
	JOIN instance i ON i.id = r.instance_id`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	var artName, artSum *string
	var artSize *int64
	err := row.Scan(&r.ID, &r.Instance, &r.Environment, &r.Operation, &r.State,
		&r.Reason, &r.Error, &r.JobID, &r.SubmittedAt, &r.StartedAt, &r.FinishedAt,
		&artName, &artSize, &artSum, &r.RequestedBy)
	if err != nil {
		return Run{}, err
	}
	if artName != nil {
		r.Artifact = &Artifact{Name: *artName, SizeBytes: *artSize, Checksum: *artSum}
	}
	return r, nil
}

// Get returns one run by id, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id int64) (Run, error) {
	r, err := scanRun(s.pool.QueryRow(ctx, runColumns+` WHERE r.id = $1`, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Run{}, ErrNotFound
	case err != nil:
		return Run{}, fmt.Errorf("runs: get %d: %w", id, err)
	}
	return r, nil
}

// ListFilter narrows List. Zero values mean "any"; unknown values are
// filters that match nothing, never errors (SPEC-014 mini-ADR 5).
type ListFilter struct {
	Instance    string
	State       string
	Environment string
	Operation   string
}

// List returns the newest 50 runs matching the filter (fields ANDed).
func (s *Service) List(ctx context.Context, f ListFilter) ([]Run, error) {
	where, args := []string{}, []any{}
	for col, val := range map[string]string{
		"i.name": f.Instance, "r.state": f.State,
		"r.environment": f.Environment, "r.operation": f.Operation,
	} {
		if val != "" {
			args = append(args, val)
			where = append(where, fmt.Sprintf("%s = $%d", col, len(args)))
		}
	}
	query := runColumns
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY r.id DESC LIMIT 50"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("runs: list: %w", err)
	}
	defer rows.Close()

	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("runs: scan run: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: list: %w", err)
	}
	return out, nil
}
