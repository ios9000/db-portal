-- +goose Up
-- AuthZ (WU-021, SPEC-021): portal-DB role store. The directory says who
-- you are; these tables say what you may do. Seeds: the dba role and ONE
-- standing grant — break-glass, because an emergency account that cannot
-- act is not an emergency account. Dev-directory grants (dba1/dba2,
-- local-dev) are applied at boot by auth mode, never here.

CREATE TABLE role (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE user_role (
    username   text        NOT NULL,
    role_id    bigint      NOT NULL REFERENCES role (id),
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (username, role_id)
);

INSERT INTO role (name) VALUES ('dba');
INSERT INTO user_role (username, role_id)
    SELECT 'break-glass', id FROM role WHERE name = 'dba';

-- Role denials land in the actor-centric auth ledger (SPEC-021 mini-ADR 3);
-- audit_event's NOT NULL run_id has no run to point at.
ALTER TABLE auth_event DROP CONSTRAINT auth_event_action_check;
ALTER TABLE auth_event ADD CONSTRAINT auth_event_action_check CHECK (action IN
    ('auth.login', 'auth.login_failed', 'auth.logout', 'auth.break_glass',
     'authz.denied'));

-- +goose Down
-- Dev-only caveat: restoring the old CHECK fails if authz.denied rows
-- exist (they cannot be deleted — auth_event is append-only by design).
ALTER TABLE auth_event DROP CONSTRAINT auth_event_action_check;
ALTER TABLE auth_event ADD CONSTRAINT auth_event_action_check CHECK (action IN
    ('auth.login', 'auth.login_failed', 'auth.logout', 'auth.break_glass'));
DROP TABLE user_role;
DROP TABLE role;
