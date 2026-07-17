-- +goose Up
-- Instance concurrency lock (SPEC-042): at most one live operation per
-- instance across ALL launch paths (button, scheduler, chain step), enforced
-- at the single choke point runs.Service.Start. A lock ROW — not a pg advisory
-- lock — is the hierarchical TTL lock ARCHITECTURE §concurrency planned for M4:
-- it survives restarts, names its holder run + actor (auditable), and carries
-- an expires_at so a leaked lock self-heals. Primary release is finalize's own
-- transaction (atomic with the terminal run state); the boot sweep reclaims a
-- crashed holder's lock by finalizing its orphaned run; expires_at is only a
-- backstop, and the acquire never steals from a still-live holder (SPEC-042
-- mini-ADR 3), so the TTL can never cause two concurrent ops.
CREATE TABLE instance_lock (
    instance_id bigint      PRIMARY KEY REFERENCES instance (id) ON DELETE CASCADE,
    run_id      bigint      NOT NULL REFERENCES run (id),
    actor       text        NOT NULL,
    acquired_at timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL
);

-- guardrail.denied records a target refused by a structural guardrail: the
-- portal self-target ban and the naive-Patroni-restore block (SPEC-042
-- mini-ADRs 5+6). It rides auth_event — the security ledger, beside
-- authz.denied — not the run-centric audit_event, because a refused target
-- never becomes a run. Extends the CHECK the way 0006 added authz.denied.
ALTER TABLE auth_event DROP CONSTRAINT auth_event_action_check;
ALTER TABLE auth_event ADD CONSTRAINT auth_event_action_check CHECK (action IN
    ('auth.login', 'auth.login_failed', 'auth.logout', 'auth.break_glass',
     'authz.denied', 'guardrail.denied'));

-- +goose Down
-- Dev-only caveat (as 0006): restoring the old CHECK fails if guardrail.denied
-- rows exist — they cannot be deleted, auth_event is append-only by design.
ALTER TABLE auth_event DROP CONSTRAINT auth_event_action_check;
ALTER TABLE auth_event ADD CONSTRAINT auth_event_action_check CHECK (action IN
    ('auth.login', 'auth.login_failed', 'auth.logout', 'auth.break_glass',
     'authz.denied'));
DROP TABLE instance_lock;
