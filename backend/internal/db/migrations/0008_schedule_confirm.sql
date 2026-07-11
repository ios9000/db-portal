-- +goose Up
-- M2-gate fix, finding 1 (WU-024): persist the prod ritual EVIDENCE, not
-- just its outcome. The executor used to fire with Confirm := instance
-- name — true by construction, so a schedule created on a test instance
-- kept auto-passing the ritual after inventory re-import promoted the
-- instance to prod. Storing what the human actually typed at creation
-- makes the fire-time check honest: promotion turns an unconfirmed
-- schedule into a visible 'error', never a silent prod dump.
ALTER TABLE schedule
    ADD COLUMN confirm text NOT NULL DEFAULT '';

-- Existing prod schedules (if any) were created through the ritual, so
-- backfill their confirm with the instance name; non-prod rows keep ''.
UPDATE schedule s SET confirm = i.name
    FROM instance i WHERE i.id = s.instance_id AND i.env = 'prod';

-- +goose Down
ALTER TABLE schedule DROP COLUMN confirm;
