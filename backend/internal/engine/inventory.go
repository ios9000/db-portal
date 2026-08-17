package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// InventoryHost is one instance's connection facts for the local engine's
// per-job inventory render (SPEC-050 mini-ADR 5). Addresses and facts only —
// NEVER credentials: connection auth is host-side (service-user SSH keys /
// ~/.pgpass, ADR-014 secrets posture).
type InventoryHost struct {
	Name     string
	Host     string // empty = no connection tuple recorded (fails closed at StartJob)
	Port     int    // 0 = unrecorded → rendered as 5432, the libpq default
	Env      string
	Platform string
	Cluster  string
}

// InventorySource resolves an instance name to its connection facts at
// StartJob time, so every job sees the fleet model as imported NOW — never a
// snapshot. Implemented by the inventory store; defined here so the engine
// package keeps zero portal dependencies (the Alarmer precedent).
type InventorySource interface {
	InventoryHost(ctx context.Context, instance string) (InventoryHost, error)
}

// renderInventory emits the per-job Ansible inventory (SPEC-050 mini-ADR 5)
// as JSON — unambiguous quoting, parsed by Ansible's stock yaml inventory
// plugin (JSON is YAML; .json is in its default extension list). Least
// privilege by construction: a `target` group holding exactly the one target,
// and a `cluster` group only when members are passed (manifests declare that
// from WU-053) — never the fleet.
func renderInventory(target InventoryHost, cluster []InventoryHost) ([]byte, error) {
	groups := map[string]any{
		"target": map[string]any{
			"hosts": map[string]any{target.Name: hostVars(target)},
		},
	}
	if len(cluster) > 0 {
		hosts := make(map[string]any, len(cluster))
		for _, h := range cluster {
			hosts[h.Name] = hostVars(h)
		}
		groups["cluster"] = map[string]any{"hosts": hosts}
	}
	b, err := json.MarshalIndent(groups, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("engine: render inventory: %w", err)
	}
	return append(b, '\n'), nil
}

// hostVars is one host's inventory vars: the ansible connection address plus
// the dbportal_* facts a playbook keys off (the port is a fact, not
// ansible_port — Ansible connects to the HOST, the playbook to Postgres).
func hostVars(h InventoryHost) map[string]any {
	port := h.Port
	if port == 0 {
		port = 5432
	}
	return map[string]any{
		"ansible_host":      h.Host,
		"dbportal_port":     port,
		"dbportal_instance": h.Name,
		"dbportal_env":      h.Env,
		"dbportal_platform": h.Platform,
		"dbportal_cluster":  h.Cluster,
	}
}
