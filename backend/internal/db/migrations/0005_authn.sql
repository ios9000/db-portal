-- +goose Up
-- AuthN (WU-020, SPEC-020): server-side sessions + append-only auth trail.
-- session stores sha256(token), never the token — a leaked table must not
-- yield usable cookies (ground rule: secrets never enter the portal DB).
-- auth_event is its own ledger: audit_event stays run-centric (NOT NULL
-- run_id/instance_id are invariants 0003/0004 hardened); same enforcement
-- split as there — triggers enforce even for the owner, REVOKE documents.

CREATE TABLE session (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash   text        NOT NULL UNIQUE,
    username     text        NOT NULL,
    display_name text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE auth_event (
    id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ts     timestamptz NOT NULL DEFAULT now(),
    actor  text        NOT NULL,
    action text        NOT NULL CHECK (action IN
        ('auth.login', 'auth.login_failed', 'auth.logout', 'auth.break_glass')),
    remote text        NOT NULL DEFAULT '',
    detail text
);

-- +goose StatementBegin
CREATE FUNCTION auth_event_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'auth_event is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER auth_event_immutable
    BEFORE UPDATE OR DELETE ON auth_event
    FOR EACH ROW EXECUTE FUNCTION auth_event_immutable();

CREATE TRIGGER auth_event_immutable_truncate
    BEFORE TRUNCATE ON auth_event
    FOR EACH STATEMENT EXECUTE FUNCTION auth_event_immutable();

REVOKE UPDATE, DELETE, TRUNCATE ON auth_event FROM CURRENT_USER;

-- +goose Down
DROP TRIGGER auth_event_immutable_truncate ON auth_event;
DROP TRIGGER auth_event_immutable ON auth_event;
DROP FUNCTION auth_event_immutable();
DROP TABLE auth_event;
DROP TABLE session;
