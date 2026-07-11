#!/usr/bin/env sh
# WU-033 (SPEC-033): fresh-clone bootstrap for the dev Semaphore compose
# service. Semaphore starts empty (BoltDB) — this script logs in as the
# admin and creates the minimal objects needed to run playbooks/smoke.yml:
# a project, a repository (pointing at the bind-mounted playbooks/ dir), a
# local/no-SSH inventory, an empty environment, a template that runs
# smoke.yml, and an API token for the portal to use. Prints the template id
# and the API token at the end.
#
# Idempotent for the project/repository/inventory/environment/template
# objects (re-running skips any that already exist by name). The API token
# is the one exception — Semaphore never exposes a previously-created
# token's full value again (see the note above its creation step below), so
# each run mints a fresh one; that's harmless and expected.
#
# Requires: curl, jq. Reads admin creds from the environment (matching
# infra/compose.yaml's SEMAPHORE_* vars) — never hardcode secrets here.
#
# Usage (from repo root, after `docker compose -f infra/compose.yaml
# --env-file .env up -d --wait semaphore`):
#   set -a; . ./.env; set +a; sh infra/semaphore-bootstrap.sh
# or simply export the SEMAPHORE_* vars yourself first.

set -eu

SEMAPHORE_URL="${PORTAL_SEMAPHORE_URL:-http://127.0.0.1:3000}"
ADMIN="${SEMAPHORE_ADMIN:?SEMAPHORE_ADMIN not set — export it or source .env first}"
ADMIN_PASSWORD="${SEMAPHORE_ADMIN_PASSWORD:?SEMAPHORE_ADMIN_PASSWORD not set — export it or source .env first}"

PROJECT_NAME="dbportal-smoke"
REPO_NAME="local-playbooks"
REPO_PATH="/playbooks"           # bind-mounted by infra/compose.yaml
INVENTORY_NAME="local"
ENVIRONMENT_NAME="empty-env"
TEMPLATE_NAME="smoke"
PLAYBOOK_FILE="smoke.yml"

COOKIE_JAR="$(mktemp)"
trap 'rm -f "$COOKIE_JAR"' EXIT

log() { printf '%s\n' "$*" >&2; }

curl_json() {
    # curl_json METHOD PATH [JSON_BODY]
    method="$1"; path="$2"; body="${3:-}"
    if [ -n "$body" ]; then
        curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X "$method" \
            -H 'Content-Type: application/json' -d "$body" \
            "${SEMAPHORE_URL}${path}"
    else
        curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X "$method" \
            "${SEMAPHORE_URL}${path}"
    fi
}

log "==> Waiting for Semaphore API at ${SEMAPHORE_URL} ..."
i=0
until curl -sS -fo /dev/null "${SEMAPHORE_URL}/api/ping" 2>/dev/null; do
    i=$((i + 1))
    if [ "$i" -ge 30 ]; then
        log "Semaphore did not answer /api/ping after 30 tries — is the compose service up?"
        exit 1
    fi
    sleep 1
done

log "==> Logging in as ${ADMIN} ..."
login_status=$(curl -sS -o /dev/null -w '%{http_code}' -c "$COOKIE_JAR" \
    -X POST -H 'Content-Type: application/json' \
    -d "{\"auth\":\"${ADMIN}\",\"password\":\"${ADMIN_PASSWORD}\"}" \
    "${SEMAPHORE_URL}/api/auth/login")
if [ "$login_status" != "204" ]; then
    log "Login failed (HTTP ${login_status}). Check SEMAPHORE_ADMIN / SEMAPHORE_ADMIN_PASSWORD"
    log "against the value the semaphore container actually booted with (first-boot-only creds —"
    log "see the GOTCHA comment in infra/compose.yaml about --env-file)."
    exit 1
fi

log "==> Ensuring project '${PROJECT_NAME}' ..."
PROJECT_ID=$(curl_json GET /api/projects | jq -r --arg n "$PROJECT_NAME" '(. // [])[] | select(.name == $n) | .id' | head -n1)
if [ -z "${PROJECT_ID:-}" ]; then
    PROJECT_ID=$(curl_json POST /api/projects "{\"name\":\"${PROJECT_NAME}\"}" | jq -r '.id')
    log "    created project id=${PROJECT_ID}"
else
    log "    exists: project id=${PROJECT_ID}"
fi

log "==> Locating the project's default 'None' access key ..."
NONE_KEY_ID=$(curl_json GET "/api/project/${PROJECT_ID}/keys" | jq -r '(. // [])[] | select(.type == "none") | .id' | head -n1)
if [ -z "${NONE_KEY_ID:-}" ]; then
    NONE_KEY_ID=$(curl_json POST "/api/project/${PROJECT_ID}/keys" \
        "{\"name\":\"none\",\"type\":\"none\",\"project_id\":${PROJECT_ID}}" | jq -r '.id')
    log "    created none-type key id=${NONE_KEY_ID}"
else
    log "    exists: none-type key id=${NONE_KEY_ID}"
fi

log "==> Ensuring repository '${REPO_NAME}' -> ${REPO_PATH} ..."
REPO_ID=$(curl_json GET "/api/project/${PROJECT_ID}/repositories" | jq -r --arg n "$REPO_NAME" '(. // [])[] | select(.name == $n) | .id' | head -n1)
if [ -z "${REPO_ID:-}" ]; then
    REPO_ID=$(curl_json POST "/api/project/${PROJECT_ID}/repositories" \
        "{\"name\":\"${REPO_NAME}\",\"project_id\":${PROJECT_ID},\"git_url\":\"${REPO_PATH}\",\"git_branch\":\"master\",\"ssh_key_id\":${NONE_KEY_ID}}" | jq -r '.id')
    log "    created repository id=${REPO_ID}"
else
    log "    exists: repository id=${REPO_ID}"
fi

log "==> Ensuring inventory '${INVENTORY_NAME}' (localhost, no SSH) ..."
INVENTORY_ID=$(curl_json GET "/api/project/${PROJECT_ID}/inventory" | jq -r --arg n "$INVENTORY_NAME" '(. // [])[] | select(.name == $n) | .id' | head -n1)
if [ -z "${INVENTORY_ID:-}" ]; then
    INVENTORY_ID=$(curl_json POST "/api/project/${PROJECT_ID}/inventory" \
        "{\"name\":\"${INVENTORY_NAME}\",\"project_id\":${PROJECT_ID},\"type\":\"static\",\"inventory\":\"localhost ansible_connection=local\\n\",\"ssh_key_id\":${NONE_KEY_ID},\"become_key_id\":${NONE_KEY_ID}}" | jq -r '.id')
    log "    created inventory id=${INVENTORY_ID}"
else
    log "    exists: inventory id=${INVENTORY_ID}"
fi

log "==> Ensuring environment '${ENVIRONMENT_NAME}' (empty) ..."
ENVIRONMENT_ID=$(curl_json GET "/api/project/${PROJECT_ID}/environment" | jq -r --arg n "$ENVIRONMENT_NAME" '(. // [])[] | select(.name == $n) | .id' | head -n1)
if [ -z "${ENVIRONMENT_ID:-}" ]; then
    ENVIRONMENT_ID=$(curl_json POST "/api/project/${PROJECT_ID}/environment" \
        "{\"name\":\"${ENVIRONMENT_NAME}\",\"project_id\":${PROJECT_ID},\"json\":\"{}\",\"env\":\"{}\"}" | jq -r '.id')
    log "    created environment id=${ENVIRONMENT_ID}"
else
    log "    exists: environment id=${ENVIRONMENT_ID}"
fi

log "==> Ensuring template '${TEMPLATE_NAME}' -> ${PLAYBOOK_FILE} ..."
TEMPLATE_ID=$(curl_json GET "/api/project/${PROJECT_ID}/templates" | jq -r --arg n "$TEMPLATE_NAME" '(. // [])[] | select(.name == $n) | .id' | head -n1)
if [ -z "${TEMPLATE_ID:-}" ]; then
    TEMPLATE_ID=$(curl_json POST "/api/project/${PROJECT_ID}/templates" \
        "{\"name\":\"${TEMPLATE_NAME}\",\"project_id\":${PROJECT_ID},\"inventory_id\":${INVENTORY_ID},\"repository_id\":${REPO_ID},\"environment_id\":${ENVIRONMENT_ID},\"playbook\":\"${PLAYBOOK_FILE}\",\"app\":\"ansible\"}" | jq -r '.id')
    log "    created template id=${TEMPLATE_ID}"
else
    log "    exists: template id=${TEMPLATE_ID}"
fi

log "==> Creating an API token for the portal ..."
# NOTE: not skip-if-exists like the objects above. GET /api/user/tokens only
# ever returns a TRUNCATED id (Semaphore masks it, like a GitHub PAT list) —
# the full, usable bearer token is returned ONCE, in the POST response body,
# and can never be recovered afterwards. So "reuse" isn't possible via the
# API; re-running this script always mints a fresh token (harmless — old
# ones just sit unused; prune via `DELETE /api/user/tokens/{id}` if desired).
API_TOKEN=$(curl_json POST /api/user/tokens | jq -r '.id')
log "    created a new API token"

log ""
log "=================================================================="
log "Bootstrap complete."
log "  Project:     ${PROJECT_NAME} (id ${PROJECT_ID})"
log "  Template:    ${TEMPLATE_NAME} (id ${TEMPLATE_ID})   <- PORTAL_SEMAPHORE_TEMPLATES=smoke:${TEMPLATE_ID}"
log "=================================================================="
printf 'SEMAPHORE_TEMPLATE_ID=%s\n' "$TEMPLATE_ID"
printf 'PORTAL_SEMAPHORE_API_TOKEN=%s\n' "$API_TOKEN"
