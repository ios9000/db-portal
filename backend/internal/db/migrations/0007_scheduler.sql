-- +goose Up
-- Scheduler (WU-022, SPEC-022): schedules are portal state; the executor is
-- a tick loop over next_fire_at (mini-ADR 1). next_fire_at is NULL iff the
-- schedule is disabled (mini-ADR 5) and is persisted pre-jittered
-- (mini-ADR 6). A schedule cannot outlive its instance; fired runs and
-- their audit rows survive independently.

CREATE TABLE schedule (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id  bigint  NOT NULL REFERENCES instance (id) ON DELETE CASCADE,
    operation    text    NOT NULL,
    cron_spec    text    NOT NULL,
    reason       text,
    enabled      boolean NOT NULL DEFAULT true,
    created_by   text    NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    next_fire_at timestamptz,
    last_fired_at    timestamptz,
    last_run_id      bigint REFERENCES run (id),
    last_fire_status text CHECK (last_fire_status IN ('fired', 'skipped_overlap', 'error'))
);

CREATE INDEX schedule_due_idx ON schedule (next_fire_at) WHERE enabled;

-- +goose Down
DROP TABLE schedule;
