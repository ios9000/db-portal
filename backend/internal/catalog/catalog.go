// Package catalog is the data-driven operation catalog (SPEC-012 mini-ADR 3):
// static Go data served over the API. It moves to storage only when
// operations multiply or grow per-instance availability rules.
package catalog

// Operation describes one runnable operation. Template and PlaybookTag are
// engine concerns and never leave the server.
type Operation struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Icon         string `json:"icon"`
	Description  string `json:"description"`
	DurationHint string `json:"duration_hint"`
	OnlineHint   string `json:"online_hint"`
	Template     string `json:"-"`
	PlaybookTag  string `json:"-"`
}

// operations is the MVP catalog: dump only (D1). Copy comes from the design
// brief's operation strip.
var operations = []Operation{
	{
		ID:           "dump",
		Label:        "Backup",
		Icon:         "💾",
		Description:  "Full backup (pg_dump), verified after completion.",
		DurationHint: "~25 min",
		OnlineHint:   "Database stays online",
		Template:     "dump",
		PlaybookTag:  "dump",
	},
}

// All returns every operation, in display order.
func All() []Operation {
	out := make([]Operation, len(operations))
	copy(out, operations)
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
