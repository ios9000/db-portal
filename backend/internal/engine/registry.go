package engine

import (
	"fmt"
	"sync"
)

// EnvClass partitions engines by blast radius. Prod and nonprod adapters
// NEVER share configuration or credentials (guardrail layer 3) — the
// registry makes that structural: one adapter instance per class.
type EnvClass string

const (
	ClassProd    EnvClass = "prod"
	ClassNonProd EnvClass = "nonprod"
)

// ClassForEnv maps an inventory environment to its engine class.
// Unknown environments error (fail closed) rather than defaulting.
func ClassForEnv(env string) (EnvClass, error) {
	switch env {
	case "prod":
		return ClassProd, nil
	case "test", "dev":
		return ClassNonProd, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownEnv, env)
	}
}

// Registry resolves the Adapter for an env class. Safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	adapters map[EnvClass]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[EnvClass]Adapter)}
}

func (r *Registry) Register(class EnvClass, a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[class] = a
}

// For returns the adapter registered for class, or ErrNoAdapter.
func (r *Registry) For(class EnvClass) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[class]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoAdapter, class)
	}
	return a, nil
}
