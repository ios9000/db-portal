-- +goose Up
-- Artifact registry (SPEC-030): dump outputs as first-class queryable rows,
-- the restore workflow's source of truth (WU-031 reads ONLY this table).
-- Run's artifact_* columns STAY: registry = what can be restored, run
-- columns = what this run produced — same values, written in the same
-- finalize transaction (SPEC-030 mini-ADR 1).
CREATE TABLE artifact (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id          bigint NOT NULL UNIQUE REFERENCES run (id), -- origin; one artifact per run (mini-ADR 3)
    name            text NOT NULL,
    size_bytes      bigint NOT NULL,
    checksum        text NOT NULL,
    retention_class text NOT NULL DEFAULT 'standard'
                        CHECK (retention_class IN ('standard', 'safety')),
    location        text, -- DORMANT until WU-034/035 (the 0003 window_warned pattern)
    created_at      timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN artifact.location IS
    'where the bytes live — NULL until WU-034 (compose volume) / WU-035 (object store)';

-- Backfill: every historical successful dump registers exactly once. The
-- three artifact_* columns are only ever written together (finalize), so
-- requiring all three skips nothing real. ON CONFLICT is belt-and-braces:
-- goose applies this once, the clause covers manual re-runs.
INSERT INTO artifact (run_id, name, size_bytes, checksum, created_at)
SELECT r.id, r.artifact_name, r.artifact_size_bytes, r.artifact_checksum,
       COALESCE(r.finished_at, r.updated_at)
FROM run r
WHERE r.state = 'success'
  AND r.artifact_name IS NOT NULL
  AND r.artifact_size_bytes IS NOT NULL
  AND r.artifact_checksum IS NOT NULL
ON CONFLICT (run_id) DO NOTHING;

-- +goose Down
DROP TABLE artifact;
