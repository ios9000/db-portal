package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// MockConfig configures a MockEngine.
type MockConfig struct {
	// Name prefixes every JobID (e.g. "mock-nonprod-3") so mis-routed jobs
	// are visible at a glance. Required.
	Name string
	// StepDelay is the pause before each script step; defaults to 200ms.
	// Tests use a few milliseconds.
	StepDelay time.Duration
}

// MockEngine is a goroutine-driven Adapter emitting realistic, timed,
// Ansible-flavored log lines. The default engine for dev and ALL tests
// forever (ADR-002). Failure injection: params["mock_fail_at"] = zero-based
// step index that fails.
type MockEngine struct {
	name  string
	delay time.Duration

	seq  atomic.Int64
	mu   sync.Mutex
	jobs map[JobID]*mockJob
}

var _ Adapter = (*MockEngine)(nil)

type mockJob struct {
	mu     sync.Mutex
	status JobStatus
	lines  []LogLine
	subs   []chan LogLine
	done   chan struct{} // closed exactly once, on reaching a terminal state

	cancelRun context.CancelFunc
}

func NewMockEngine(cfg MockConfig) *MockEngine {
	delay := cfg.StepDelay
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}
	return &MockEngine{
		name:  cfg.Name,
		delay: delay,
		jobs:  make(map[JobID]*mockJob),
	}
}

// StartJob registers the job (state queued) and launches its runner
// goroutine. The runner lives beyond ctx: cancelling the request that
// started a job must not kill the job — that is what Cancel is for.
func (e *MockEngine) StartJob(_ context.Context, template string, params map[string]string) (JobID, error) {
	id := JobID(fmt.Sprintf("%s-%d", e.name, e.seq.Add(1)))

	p := make(map[string]string, len(params))
	for k, v := range params {
		p[k] = v
	}

	runCtx, cancel := context.WithCancel(context.Background())
	j := &mockJob{
		status:    JobStatus{ID: id, State: StateQueued},
		done:      make(chan struct{}),
		cancelRun: cancel,
	}

	e.mu.Lock()
	e.jobs[id] = j
	e.mu.Unlock()

	go e.run(runCtx, j, template, p)
	return id, nil
}

func (e *MockEngine) Status(_ context.Context, id JobID) (JobStatus, error) {
	j, err := e.job(id)
	if err != nil {
		return JobStatus{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, nil
}

// StreamLogs replays history, then follows live output. The channel closes
// when the job finishes or ctx is cancelled.
func (e *MockEngine) StreamLogs(ctx context.Context, id JobID) (<-chan LogLine, error) {
	j, err := e.job(id)
	if err != nil {
		return nil, err
	}

	j.mu.Lock()
	// Capacity covers full replay plus any live lines a mock script can
	// still emit, so sends never block (see emit).
	ch := make(chan LogLine, len(j.lines)+64)
	for _, l := range j.lines {
		ch <- l
	}
	if j.status.State.Terminal() {
		close(ch)
		j.mu.Unlock()
		return ch, nil
	}
	j.subs = append(j.subs, ch)
	j.mu.Unlock()

	go func() {
		select {
		case <-j.done:
			// finish() closed ch after removing it from subs.
		case <-ctx.Done():
			// Whoever removes the sub from the list closes it — never both.
			if j.removeSub(ch) {
				close(ch)
			}
		}
	}()
	return ch, nil
}

// Cancel stops a running job; cancelling a terminal job is a no-op.
// The transition to StateCanceled is asynchronous — observe it via
// Status or the log stream closing.
func (e *MockEngine) Cancel(_ context.Context, id JobID) error {
	j, err := e.job(id)
	if err != nil {
		return err
	}
	j.cancelRun()
	return nil
}

func (e *MockEngine) job(id JobID) (*mockJob, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, ok := e.jobs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownJob, id)
	}
	return j, nil
}

// run drives one job from queued to a terminal state.
func (e *MockEngine) run(ctx context.Context, j *mockJob, template string, params map[string]string) {
	steps := scriptFor(template, params)

	failAt := -1
	if v, ok := params["mock_fail_at"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			failAt = n
		}
	}

	j.mu.Lock()
	j.status.State = StateRunning
	j.status.Started = time.Now()
	j.mu.Unlock()
	j.emit(fmt.Sprintf("PLAY [%s] (job %s) %s", template, j.status.ID, stars))

	for i, step := range steps {
		select {
		case <-ctx.Done():
			j.emit("RUNNING HANDLER [cleanup : releasing locks] ok")
			j.finish(StateCanceled, "canceled by operator", nil)
			return
		case <-time.After(e.delay):
		}

		if i == failAt {
			j.emit(fmt.Sprintf("TASK [%s] FAILED! => {\"msg\": \"injected failure at step %d\"}", step, i))
			j.emit(fmt.Sprintf("PLAY RECAP %s ok=%d failed=1", stars, i))
			j.finish(StateFailed, fmt.Sprintf("step %d (%s) failed", i, step), nil)
			return
		}
		j.emit(fmt.Sprintf("TASK [%s] ok", step))
	}
	j.emit(fmt.Sprintf("PLAY RECAP %s ok=%d failed=0", stars, len(steps)))

	var artifact *Artifact
	if template == "dump" {
		sum := sha256.Sum256([]byte(j.status.ID))
		instance := params["instance"]
		if instance == "" {
			instance = "instance"
		}
		artifact = &Artifact{
			Name:      fmt.Sprintf("%s-%s.dump", instance, j.status.ID),
			SizeBytes: 1 << 20,
			Checksum:  hex.EncodeToString(sum[:]),
		}
	}
	j.finish(StateSuccess, "", artifact)
}

const stars = "*******************"

// scriptFor returns the mock task list for a template. Unknown templates
// get a generic script — template naming is a catalog concern (WU-012),
// not an engine guardrail.
func scriptFor(template string, params map[string]string) []string {
	target := params["instance"]
	if target == "" {
		target = "target-instance"
	}
	switch template {
	case "dump":
		return []string{
			"preflight : ping " + target,
			"preflight : check disk space",
			"dump : run pg_dump --format=custom",
			"dump : sha256 checksum artifact",
			"dump : register artifact metadata",
		}
	case "restore":
		return []string{
			"preflight : ping " + target,
			"preflight : verify artifact checksum",
			"safety : pre-restore dump of target",
			"restore : run pg_restore",
			"restore : post-restore sanity queries",
		}
	default:
		return []string{
			"preflight : ping " + target,
			template + " : execute",
			"report : collect results",
		}
	}
}

// emit appends a line to history and fans it out to live subscribers.
// Sends are non-blocking: subscriber channels are sized so a mock script
// can never fill them, and dropping is preferable to a stuck engine.
func (j *mockJob) emit(line string) {
	l := LogLine{TS: time.Now(), Line: line}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, l)
	for _, ch := range j.subs {
		select {
		case ch <- l:
		default:
		}
	}
}

// finish moves the job to a terminal state, closes subscriber channels
// and the done channel. Called exactly once per job.
func (j *mockJob) finish(state JobState, errMsg string, artifact *Artifact) {
	j.mu.Lock()
	j.status.State = state
	j.status.Finished = time.Now()
	j.status.Error = errMsg
	j.status.Artifact = artifact
	subs := j.subs
	j.subs = nil
	j.mu.Unlock()

	for _, ch := range subs {
		close(ch)
	}
	close(j.done)
}

// removeSub detaches ch; the caller closes it only if removal succeeded.
func (j *mockJob) removeSub(ch chan LogLine) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i, s := range j.subs {
		if s == ch {
			j.subs = append(j.subs[:i], j.subs[i+1:]...)
			return true
		}
	}
	return false
}
