// Package engine is the ExecutionAdapter seam (ADR-002): ALL engine access
// goes through the Adapter interface, resolved per environment class via
// the Registry. MockEngine is the only implementation until WU-033
// (SemaphoreAdapter); portal code must never reference an implementation
// concretely.
package engine

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrUnknownJob is returned for a JobID this adapter never issued.
	ErrUnknownJob = errors.New("engine: unknown job")
	// ErrNoAdapter is returned by the Registry for an unregistered env class.
	ErrNoAdapter = errors.New("engine: no adapter registered for env class")
	// ErrUnknownEnv is returned when an environment name maps to no class:
	// a mis-routed job must fail closed (guardrail layer 3), never default.
	ErrUnknownEnv = errors.New("engine: unknown environment")
)

// JobID identifies a job within the adapter that issued it. IDs must never
// alias across adapter instances (e.g. process restarts): a stale JobID has
// to fail with ErrUnknownJob, not resolve to some newer job — orphaned runs
// otherwise serve another run's logs (WU-013).
type JobID string

// JobState is the lifecycle of a single engine job.
type JobState string

const (
	StateQueued   JobState = "queued"
	StateRunning  JobState = "running"
	StateSuccess  JobState = "success"
	StateFailed   JobState = "failed"
	StateCanceled JobState = "canceled"
)

// Terminal reports whether no further state changes can occur.
func (s JobState) Terminal() bool {
	return s == StateSuccess || s == StateFailed || s == StateCanceled
}

// LogLine is one timestamped line of job output.
type LogLine struct {
	TS   time.Time
	Line string
}

// Artifact is metadata for an output a job produced (e.g. a dump file).
// Immutable once set on a JobStatus.
type Artifact struct {
	Name      string
	SizeBytes int64
	Checksum  string // sha256, hex
	// Location is where the bytes live: a filesystem path on the shared
	// artifact volume today (WU-034), an object URL after WU-035. Empty for
	// the mock (metadata-only). Stored on the registry row (artifact.location,
	// SPEC-030), never exposed by the API.
	Location string
}

// JobStatus is a point-in-time snapshot of a job.
type JobStatus struct {
	ID       JobID
	State    JobState
	Started  time.Time // zero until the job left the queue
	Finished time.Time // zero until State is terminal
	Error    string    // failure reason, set when State == StateFailed
	Artifact *Artifact // set on success if the job produced an artifact
}

// Adapter is the ExecutionAdapter interface (ADR-002). Implementations
// must be safe for concurrent use.
//
// StreamLogs replays the job's full history, then follows live output;
// the channel closes when the job reaches a terminal state or ctx is
// cancelled. It may be called any number of times, including after the
// job finished.
type Adapter interface {
	StartJob(ctx context.Context, template string, params map[string]string) (JobID, error)
	Status(ctx context.Context, id JobID) (JobStatus, error)
	StreamLogs(ctx context.Context, id JobID) (<-chan LogLine, error)
	Cancel(ctx context.Context, id JobID) error
}
