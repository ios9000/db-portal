// Package version holds the single source of the portal's version string.
package version

// Version is the portal build version (semver).
const Version = "0.0.1"

// Commit and BuildDate are stamped by infra/build-release.sh via -ldflags at
// release-build time (WU-046) so a deployed pilot binary is traceable to a
// commit. A plain `go build` / `go run` leaves them "unknown", which is honest
// for a dev binary. Vars (not consts) so -ldflags -X can set them.
var (
	Commit    = "unknown"
	BuildDate = "unknown"
)
