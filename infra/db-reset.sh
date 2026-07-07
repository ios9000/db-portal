#!/usr/bin/env sh
# Drop and recreate the portal database (idempotent). Dev only.
set -e
DB="${PORTAL_DB_NAME:-portal}"
U="${PORTAL_DB_USER:-portal}"
docker compose -f infra/compose.yaml exec -T postgres psql -U "$U" -d postgres \
  -c "DROP DATABASE IF EXISTS \"$DB\" WITH (FORCE);" \
  -c "CREATE DATABASE \"$DB\" OWNER \"$U\";"
echo "db-reset: \"$DB\" recreated"
