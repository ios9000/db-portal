// Package restore assembles the restore workflow (SPEC-031, D4): a
// kind=restore chain of three steps — verify → safety_dump → restore — that
// the portal fires through the chain engine (SPEC-032). This package holds the
// RECIPE only: a pure function turning a source artifact into the fixed,
// unconditional step list. The chain.Service runs it; POST /api/restore
// assembles it. The safety dump has no skip affordance because it is always
// present in the returned steps — "institutionalized" as code, not a runtime
// check (the motivating incident, D1).
package restore

import (
	"strconv"

	"github.com/ios9000/db-portal/backend/internal/chain"
)

// Kind is the chain kind for restores. Mail subjects and the UI read it.
const Kind = "restore"

// Step operations, in fire order. Named so tests and the assembler agree on
// the recipe without magic strings.
const (
	OpVerify     = "verify"
	OpSafetyDump = "safety_dump"
	OpRestore    = "restore"
)

// Steps is the restore recipe (SPEC-031 mini-ADR 1+4): verify the source
// artifact, take an UNCONDITIONAL safety dump of the target, then restore.
// The order matters — a bad artifact halts at verify BEFORE the safety dump or
// restore touch the target. The artifact lineage (id + checksum + name) rides
// the verify and restore step params (ids and checksums only, never secrets —
// SPEC-032); the safety dump carries none, it dumps the target as-is.
func Steps(artifactID int64, checksum, name string) []chain.StepSpec {
	lineage := map[string]string{
		"artifact_id":   strconv.FormatInt(artifactID, 10),
		"checksum":      checksum,
		"artifact_name": name,
	}
	return []chain.StepSpec{
		{Operation: OpVerify, Params: cloneParams(lineage)},
		{Operation: OpSafetyDump},
		{Operation: OpRestore, Params: cloneParams(lineage)},
	}
}

// cloneParams gives each step its own map so a later mutation of one step's
// params (e.g. a test injecting mock_fail_at) can't bleed into another.
func cloneParams(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
