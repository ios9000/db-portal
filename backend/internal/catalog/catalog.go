// Package catalog is the data-driven operation catalog (SPEC-012 mini-ADR 3):
// static Go data served over the API. It moves to storage only when
// operations multiply or grow per-instance availability rules.
package catalog

// Operation describes one runnable operation. Template and PlaybookTag are
// engine concerns and never leave the server.
//
// Launchable marks an operation the user may fire directly (the run-now drawer
// and the scheduler); non-launchable operations exist only as chain steps
// (SPEC-031 mini-ADR 3 — verify/safety_dump/restore). RetentionClass is the
// class stamped on any artifact the operation registers (SPEC-031 mini-ADR 2):
// empty for operations that produce none.
type Operation struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Icon           string `json:"icon"`
	Description    string `json:"description"`
	DurationHint   string `json:"duration_hint"`
	OnlineHint     string `json:"online_hint"`
	Template       string `json:"-"`
	PlaybookTag    string `json:"-"`
	Launchable     bool   `json:"-"`
	RetentionClass string `json:"-"`
}

// operations is the MVP catalog (D4): a run-now/schedulable `dump`, plus the
// three internal steps the restore chain assembles (SPEC-031). Only launchable
// operations reach the UI; the rest fire solely as chain steps. Copy comes
// from the design brief's operation strip.
var operations = []Operation{
	{
		ID:             "dump",
		Label:          "Backup",
		Icon:           "💾",
		Description:    "Full backup (pg_dump), verified after completion.",
		DurationHint:   "~25 min",
		OnlineHint:     "Database stays online",
		Template:       "dump",
		PlaybookTag:    "dump",
		Launchable:     true,
		RetentionClass: "standard",
	},
	// Restore-chain steps (SPEC-031). Portal-assembled only: not launchable,
	// so POST /api/runs and the scheduler refuse them at the door.
	{
		ID:          "verify",
		Label:       "Verify checksum",
		Icon:        "🔎",
		Description: "Verify the artifact's checksum before touching the target.",
		Template:    "verify",
		PlaybookTag: "verify",
	},
	{
		ID:             "safety_dump",
		Label:          "Safety backup",
		Icon:           "🛟",
		Description:    "Unconditional pre-restore backup of the target.",
		Template:       "dump", // same dump playbook; the safety INTENT differs
		PlaybookTag:    "dump",
		RetentionClass: "safety",
	},
	{
		ID:          "restore",
		Label:       "Restore",
		Icon:        "♻️",
		Description: "Restore a registered artifact onto the target.",
		Template:    "restore",
		PlaybookTag: "restore",
	},
}

// All returns the launchable operations, in display order — the user-facing
// catalog (GET /api/operations, the launch drawer). Internal chain-step
// operations are reachable only through ByID (Start/chain validation).
func All() []Operation {
	out := make([]Operation, 0, len(operations))
	for _, op := range operations {
		if op.Launchable {
			out = append(out, op)
		}
	}
	return out
}

// ByID returns the operation with the given id.
func ByID(id string) (Operation, bool) {
	for _, op := range operations {
		if op.ID == id {
			return op, true
		}
	}
	return Operation{}, false
}
