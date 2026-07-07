-- +goose Up
-- Runs + audit (SPEC-012). run = operational state, app-mutable.
-- audit_event = the ARCHITECTURE §5 record, one row per transition,
-- append-only from this first migration (trigger enforces it even for the
-- table owner; the REVOKE documents intent for non-owner roles).

CREATE TABLE run (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id         bigint NOT NULL REFERENCES instance (id),
    operation           text NOT NULL,
    environment         text NOT NULL, -- stamped at submit (guardrail layer 4)
    engine_class        text NOT NULL,
    playbook_tag        text NOT NULL,
    job_id              text,
    state               text NOT NULL CHECK
                            (state IN ('queued', 'running', 'success', 'failed', 'canceled')),
    reason              text,
    error               text,
    artifact_name       text,
    artifact_size_bytes bigint,
    artifact_checksum   text,
    submitted_at        timestamptz NOT NULL DEFAULT now(),
    started_at          timestamptz,
    finished_at         timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX run_submitted_at_idx ON run (submitted_at DESC);

CREATE TABLE audit_event (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ts            timestamptz NOT NULL DEFAULT now(),
    actor         text NOT NULL,
    action        text NOT NULL, -- 'run.submitted' | 'run.finished'
    run_id        bigint NOT NULL REFERENCES run (id),
    instance_id   bigint NOT NULL REFERENCES instance (id),
    environment   text NOT NULL,
    playbook_tag  text NOT NULL,
    params_digest text NOT NULL, -- sha256 hex of canonical params JSON
    final_status  text,          -- NULL on submitted; run state on finished
    window_warned boolean NOT NULL DEFAULT false -- semantics arrive in WU-023
);

-- +goose StatementBegin
CREATE FUNCTION audit_event_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_event is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER audit_event_immutable
    BEFORE UPDATE OR DELETE ON audit_event
    FOR EACH ROW EXECUTE FUNCTION audit_event_immutable();

REVOKE UPDATE, DELETE ON audit_event FROM CURRENT_USER;

-- +goose Down
DROP TABLE audit_event;
DROP FUNCTION audit_event_immutable;
DROP TABLE run;
