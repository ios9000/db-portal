-- +goose Up
-- Baseline: marks a migrated database. Domain tables arrive with their WUs.
CREATE TABLE app_meta (
    key   text PRIMARY KEY,
    value text NOT NULL
);
INSERT INTO app_meta (key, value) VALUES ('schema_baseline', '0001');

-- +goose Down
DROP TABLE app_meta;
