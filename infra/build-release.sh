#!/bin/sh
# build:release (WU-006, ADR-010): one static binary carrying the SPA.
# frontend build -> copy dist into the embed dir -> go build -trimpath.
# WU-046: stamp the commit + build date into the binary (traceable pilot
# artifact) and emit a SHA256 checksum next to it.
set -eu
cd "$(dirname "$0")/.."

(cd frontend && npm run build)

dist=backend/internal/webui/dist
rm -rf "$dist"
cp -R frontend/dist "$dist"
touch "$dist/.gitkeep" # re-create the tracked embed anchor after the wipe

# Build provenance: git commit (short, +"-dirty" when the tree is modified) and
# a UTC build date, injected into internal/version via -ldflags -X. Falls back
# gracefully outside a git checkout.
commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
if [ "$commit" != "unknown" ] && ! git diff --quiet 2>/dev/null; then
  commit="${commit}-dirty"
fi
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
verpkg=github.com/ios9000/db-portal/backend/internal/version
ldflags="-s -w -X ${verpkg}.Commit=${commit} -X ${verpkg}.BuildDate=${build_date}"

(cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o bin/portal ./cmd/portal)

# Checksum the artifact so a deploy can verify the bytes it received.
( cd backend/bin && sha256sum portal > portal.sha256 )

ls -lh backend/bin/portal
cat backend/bin/portal.sha256
./backend/bin/portal version
