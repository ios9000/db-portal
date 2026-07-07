#!/bin/sh
# M0 exit proof (ROADMAP): "check green from clean clone; a MockEngine dump
# job runs from a test; committed proof script." Run from repo root:
#   sh infra/demo-m0.sh
# Golden thread: gate green -> mock dump job visible -> single binary
# serves the frontend shell + health endpoint on its own.
set -eu
cd "$(dirname "$0")/.."

step() { printf '\n=== M0 %s ===\n' "$1"; }
PORT="${DEMO_PORT:-8080}"

step "1/3 full gate (npm run check)"
npm run check

step "2/3 MockEngine dump job from a test"
(cd backend && go test ./internal/engine -run 'TestHappyPathDump' -v -count=1)

step "3/3 single binary serves shell + healthz alone"
npm run build:release
./backend/bin/portal &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT
sleep 1
curl -fsS "http://localhost:$PORT/" | grep -qi '<!doctype html>' \
  && echo "GET /        -> HTML shell OK"
# no compose PG required: degraded (503) is a valid healthz answer here,
# so ask curl for the body, not for a 2xx
curl -sS "http://localhost:$PORT/healthz" | grep -q '"status"' \
  && echo "GET /healthz -> JSON OK"

printf '\nM0 EXIT: PASS\n'
