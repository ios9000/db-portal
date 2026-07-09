-- +goose Up
-- Audit hardening (WU-017, m1-gate items 4-5).
-- 0003's row-level trigger never fires on TRUNCATE, so the table owner could
-- still erase the whole trail in one statement. Same enforcement split as
-- 0003: the statement trigger enforces even for the owner, the REVOKE
-- documents intent for non-owner roles.
-- job_id gives the run<->engine-job linkage a forensic anchor (ARCHITECTURE
-- §5): run.job_id is app-mutable, the audit copy is not. Stamped on
-- run.finished only — NULL at submit is honest, the id doesn't exist yet.

CREATE TRIGGER audit_event_immutable_truncate
    BEFORE TRUNCATE ON audit_event
    FOR EACH STATEMENT EXECUTE FUNCTION audit_event_immutable();

REVOKE TRUNCATE ON audit_event FROM CURRENT_USER;

ALTER TABLE audit_event ADD COLUMN job_id text;

-- +goose Down
ALTER TABLE audit_event DROP COLUMN job_id;
GRANT TRUNCATE ON audit_event TO CURRENT_USER;
DROP TRIGGER audit_event_immutable_truncate ON audit_event;
