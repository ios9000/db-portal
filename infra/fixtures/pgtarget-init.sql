-- WU-034 (SPEC-034): seed data for the compose `pgtarget` database — a small
-- but non-trivial schema so pg_dump produces a real, restorable artifact and
-- WU-036's rehearsal has data to destroy + restore. Runs once, as the DB
-- owner, from docker-entrypoint-initdb.d on the fresh pgtargetdata volume.

CREATE TABLE widget (
    id         integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text NOT NULL,
    qty        integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO widget (name, qty) VALUES
    ('alpha', 10),
    ('beta', 20),
    ('gamma', 30),
    ('delta', 40);

CREATE TABLE ledger (
    id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entry  text NOT NULL,
    amount numeric(12, 2) NOT NULL
);

INSERT INTO ledger (entry, amount)
SELECT 'entry ' || g, (g * 1.5)::numeric(12, 2)
FROM generate_series(1, 200) AS g;

-- A view + index so the dump carries more than flat tables.
CREATE INDEX ledger_amount_idx ON ledger (amount);

CREATE VIEW ledger_totals AS
SELECT count(*) AS rows, sum(amount) AS total FROM ledger;
