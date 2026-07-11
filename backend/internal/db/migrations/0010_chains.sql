-- +goose Up
-- Chain engine (WU-032, SPEC-032): sequential step execution over runs (D5).
-- A chain halts on step failure and resumes from the failed step; it never
-- fails terminally (state CHECK has no 'failed'). confirm is the stored
-- creation-time prod-ritual evidence, fired verbatim on every step (the
-- 0008 schedule.confirm pattern). Steps carry no state column: step status
-- is DERIVED from the linked run (run_id NULL = pending) — the run's
-- transitions are already single-finalizer-guarded (SPEC-032 mini-ADR 4).
-- FK direction chain_step -> run keeps `run` untouched (mini-ADR 2); resume
-- re-points run_id at the new attempt, superseded runs keep their audit rows.

CREATE TABLE chain (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        text   NOT NULL,
    instance_id bigint NOT NULL REFERENCES instance (id),
    created_by  text   NOT NULL,
    confirm     text   NOT NULL DEFAULT '',
    reason      text,
    state       text   NOT NULL DEFAULT 'running' CHECK (state IN ('running', 'halted', 'success')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    halted_at   timestamptz,
    finished_at timestamptz
);

CREATE TABLE chain_step (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chain_id  bigint NOT NULL REFERENCES chain (id) ON DELETE CASCADE,
    seq       int    NOT NULL,
    operation text   NOT NULL,
    params    jsonb  NOT NULL DEFAULT '{}',
    run_id    bigint UNIQUE REFERENCES run (id),
    UNIQUE (chain_id, seq)
);

-- +goose Down
DROP TABLE chain_step;
DROP TABLE chain;
