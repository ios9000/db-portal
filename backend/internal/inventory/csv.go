// Package inventory implements SPEC-010: the cluster/instance tables'
// idempotent CSV import with per-row validation and quarantine.
package inventory

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ios9000/db-portal/backend/internal/engine"
)

// header is the exact required CSV header (SPEC-010: order fixed).
var header = []string{
	"instance_name", "cluster_name", "env", "platform",
	"pg_version", "size_gb", "owner", "maintenance_window",
}

// headerConn is header plus the optional trailing connection-tuple columns
// (SPEC-050 mini-ADR 5, WU-051). A file carries the tuple columns or it
// doesn't — per-file, never per-row; absent columns import as NULL.
var headerConn = append(append([]string{}, header...), "host", "port")

// nameRE constrains instance/cluster names (DNS-label-ish): they become
// CLI, URL and audit identifiers.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Row is one accepted CSV data row.
type Row struct {
	Line              int    // 1-based line number in the file (header = line 1)
	Raw               string // the CSV line verbatim
	InstanceName      string
	ClusterName       string
	Env               string
	Platform          string
	PGVersion         string
	SizeGB            *string // validated finite number, canonical decimal text; nil when empty
	Owner             string
	MaintenanceWindow *string // raw string, semantics owned by WU-022 (O-3); nil when empty
	Host              *string // connection tuple (SPEC-050 mini-ADR 5); nil when absent/empty
	Port              *int    // validated 1..65535; nil when absent/empty
}

// Reject is a quarantined CSV data row with every reason that applies.
type Reject struct {
	Line    int
	Raw     string
	Reasons []string
}

// ParseResult separates accepted rows from quarantined ones. Duplicate
// instance names keep the first accepted row (SPEC-010 behavior 5).
type ParseResult struct {
	Total   int // data rows seen (accepted + rejected)
	Rows    []Row
	Rejects []Reject
}

// Parse reads and validates a whole inventory CSV. File-level problems
// (unreadable input, missing/wrong header, empty file) return an error;
// per-row problems quarantine the row and parsing keeps going.
//
// The file is processed line by line so each reject can store its raw line
// verbatim; quoted fields therefore must not contain newlines (no contract
// field legitimately does).
func Parse(r io.Reader) (*ParseResult, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("inventory: read: %w", err)
		}
		return nil, errors.New("inventory: empty file (missing header)")
	}
	got, err := splitLine(sc.Text())
	hasConn := err == nil && slices.Equal(got, headerConn)
	if err != nil || (!slices.Equal(got, header) && !hasConn) {
		return nil, fmt.Errorf("inventory: bad header: want exactly %q (optionally + %q)",
			strings.Join(header, ","), "host,port")
	}

	res := &ParseResult{}
	accepted := map[string]bool{}          // instance names that will import
	clusterPlatform := map[string]string{} // cluster -> platform, from accepted rows
	line := 1
	for sc.Scan() {
		line++
		raw := sc.Text()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		res.Total++

		row, reasons := parseRow(raw, hasConn)
		// Would-import checks: only fully valid rows reserve a name or
		// establish a cluster's platform (first row wins).
		if len(reasons) == 0 {
			if accepted[row.InstanceName] {
				reasons = append(reasons, "duplicate instance_name in file")
			}
			if p, ok := clusterPlatform[row.ClusterName]; ok && p != row.Platform {
				reasons = append(reasons, platformConflict(row.ClusterName, p))
			}
		}
		if len(reasons) > 0 {
			res.Rejects = append(res.Rejects, Reject{Line: line, Raw: raw, Reasons: reasons})
			continue
		}

		accepted[row.InstanceName] = true
		clusterPlatform[row.ClusterName] = row.Platform
		row.Line, row.Raw = line, raw
		res.Rows = append(res.Rows, row)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("inventory: read: %w", err)
	}
	return res, nil
}

// parseRow validates one data line, accumulating every applicable reason
// rather than stopping at the first (SPEC-010 behavior 4). hasConn says the
// file's header declared the trailing host,port columns.
func parseRow(raw string, hasConn bool) (Row, []string) {
	fields, err := splitLine(raw)
	if err != nil {
		return Row{}, []string{"malformed CSV: " + err.Error()}
	}
	want := header
	if hasConn {
		want = headerConn
	}
	if len(fields) != len(want) {
		return Row{}, []string{fmt.Sprintf("wrong column count: got %d, want %d", len(fields), len(want))}
	}

	row := Row{
		InstanceName: fields[0],
		ClusterName:  fields[1],
		Env:          fields[2],
		Platform:     fields[3],
		PGVersion:    fields[4],
		Owner:        fields[6],
	}
	var reasons []string
	reasons = append(reasons, checkName("instance_name", row.InstanceName)...)
	reasons = append(reasons, checkName("cluster_name", row.ClusterName)...)
	// The engine registry is the single authority on valid envs — the CSV
	// layer must never accept an env the engine cannot classify.
	if _, err := engine.ClassForEnv(row.Env); err != nil {
		reasons = append(reasons, fmt.Sprintf("unknown env %q (want dev|test|prod)", row.Env))
	}
	if row.Platform != "k8s_patroni" && row.Platform != "vm" {
		reasons = append(reasons, fmt.Sprintf("unknown platform %q (want k8s_patroni|vm)", row.Platform))
	}
	if row.PGVersion == "" {
		reasons = append(reasons, "pg_version is required")
	}
	if s := fields[5]; s != "" {
		// ParseFloat alone would admit "nan"/"inf", which Postgres numeric
		// stores verbatim — require a finite number.
		if v, err := strconv.ParseFloat(s, 64); err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			reasons = append(reasons, fmt.Sprintf("size_gb must be numeric, got %q", s))
		} else {
			// Keep the canonical decimal rendering, not the raw text: Go
			// accepts forms Postgres rejects (hex floats — the ::numeric cast
			// would abort the whole import) or normalizes ('1e2' -> '100' —
			// the unchanged-row compare would report "updated" forever).
			if v == 0 {
				v = 0 // fold -0: Postgres numeric has no signed zero
			}
			c := strconv.FormatFloat(v, 'f', -1, 64)
			row.SizeGB = &c
		}
	}
	if row.Owner == "" {
		reasons = append(reasons, "owner is required")
	}
	if w := fields[7]; w != "" {
		row.MaintenanceWindow = &w
	}
	if hasConn {
		reasons = append(reasons, parseConn(&row, fields[8], fields[9])...)
	}
	return row, reasons
}

// parseConn validates the optional connection tuple (SPEC-050 mini-ADR 5).
// Both empty is fine (NULL tuple — the local engine fails closed at launch);
// a port needs a host to attach to; a bare host is allowed (the render
// defaults the port to 5432, the libpq default).
func parseConn(row *Row, host, port string) []string {
	var reasons []string
	if host != "" {
		if strings.ContainsAny(host, " \t") || len(host) > 253 {
			reasons = append(reasons, fmt.Sprintf("host must be a hostname or address, got %q", host))
		} else {
			row.Host = &host
		}
	}
	if port != "" {
		p, err := strconv.Atoi(port)
		switch {
		case err != nil || p < 1 || p > 65535:
			reasons = append(reasons, fmt.Sprintf("port must be an integer in 1..65535, got %q", port))
		case host == "":
			reasons = append(reasons, "port without host")
		default:
			row.Port = &p
		}
	}
	return reasons
}

func checkName(field, v string) []string {
	switch {
	case v == "":
		return []string{field + " is required"}
	case !nameRE.MatchString(v):
		return []string{field + " must match " + nameRE.String()}
	}
	return nil
}

func platformConflict(cluster, existing string) string {
	return fmt.Sprintf("cluster platform conflict: %q is already %q", cluster, existing)
}

// splitLine parses a single CSV line into fields, tolerating any field count
// (the caller enforces the contract's count).
func splitLine(line string) ([]string, error) {
	r := csv.NewReader(strings.NewReader(line))
	r.FieldsPerRecord = -1
	return r.Read()
}
