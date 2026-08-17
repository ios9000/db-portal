-- +goose Up
-- Connection tuple for the local engine's per-job inventory render
-- (SPEC-050 mini-ADR 5, WU-051). Nullable on purpose: the CSV columns are
-- optional, and a missing tuple fails closed at StartJob — a run must never
-- silently target localhost. NO credentials here, ever (ADR-014 secrets
-- posture): auth is host-side (service-user SSH keys / ~/.pgpass).
ALTER TABLE instance
    ADD COLUMN host text,
    ADD COLUMN port int CONSTRAINT instance_port_range CHECK (port BETWEEN 1 AND 65535);

-- +goose Down
ALTER TABLE instance
    DROP COLUMN host,
    DROP COLUMN port;
