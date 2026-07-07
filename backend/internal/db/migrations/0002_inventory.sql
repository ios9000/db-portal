-- +goose Up
-- Inventory (SPEC-010): the portal DB's working copy of the estate.
-- instance.name is the natural key for import idempotency; env is the ONLY
-- env source in the system (engine class derives via engine.ClassForEnv).

CREATE TABLE cluster (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name          text NOT NULL UNIQUE,
    platform      text NOT NULL CHECK (platform IN ('k8s_patroni', 'vm')),
    patroni_scope text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE instance (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name               text NOT NULL UNIQUE,
    cluster_id         bigint NOT NULL REFERENCES cluster (id),
    env                text NOT NULL CHECK (env IN ('dev', 'test', 'prod')),
    pg_version         text NOT NULL,
    size_gb            numeric,
    owner              text NOT NULL,
    maintenance_window text, -- raw string; WU-022 owns semantics (O-3)
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- One row per import run. Bookkeeping only — the formal append-only audit
-- trail attaches to operations and arrives in WU-012.
CREATE TABLE inventory_import (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ts                timestamptz NOT NULL DEFAULT now(),
    filename          text NOT NULL,
    rows_total        int NOT NULL,
    rows_new          int NOT NULL,
    rows_updated      int NOT NULL,
    rows_unchanged    int NOT NULL,
    rows_quarantined  int NOT NULL
);

CREATE TABLE inventory_import_reject (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    import_id  bigint NOT NULL REFERENCES inventory_import (id),
    row_number int NOT NULL,
    raw        text NOT NULL,
    reasons    text[] NOT NULL
);

-- +goose Down
DROP TABLE inventory_import_reject;
DROP TABLE inventory_import;
DROP TABLE instance;
DROP TABLE cluster;
