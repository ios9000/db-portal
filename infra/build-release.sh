#!/bin/sh
# build:release (WU-006, ADR-010): one static binary carrying the SPA.
# frontend build -> copy dist into the embed dir -> go build -trimpath.
set -eu
cd "$(dirname "$0")/.."

(cd frontend && npm run build)

dist=backend/internal/webui/dist
rm -rf "$dist"
cp -R frontend/dist "$dist"
touch "$dist/.gitkeep" # re-create the tracked embed anchor after the wipe

(cd backend && CGO_ENABLED=0 go build -trimpath -o bin/portal ./cmd/portal)

ls -lh backend/bin/portal
