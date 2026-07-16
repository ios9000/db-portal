-- +goose Up
-- job_id uniqueness (M3-gate item 7 / WU-040). ReconcileByJobID resolves a run
-- by its engine task id (the webhook's acceleration path, SPEC-033 mini-ADR 2).
-- The column was a bare `text` (0003) with no uniqueness, so a reused id — a
-- Semaphore BoltDB wipe (`docker compose down -v`) replaying task ids while a
-- run was still in-flight — could let a later task's status/artifact be
-- attributed to a stale run. A partial UNIQUE index makes a duplicate non-null
-- id impossible at the schema layer. PostgreSQL treats NULLs as distinct, so
-- queued/unstarted runs (job_id IS NULL) are unaffected and multiple may
-- coexist.
CREATE UNIQUE INDEX run_job_id_unique ON run (job_id) WHERE job_id IS NOT NULL;

-- +goose Down
DROP INDEX run_job_id_unique;
