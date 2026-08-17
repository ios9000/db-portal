package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// LocalConfig configures a LocalAdapter (SPEC-050). Library and Workdir are
// required; the rest defaults per the SPEC-050 config table.
type LocalConfig struct {
	// AnsibleBin is the ansible-playbook executable — a bare name resolved
	// via PATH or an explicit path. Tests point it at a stub binary, which is
	// how the whole adapter stays provable in CI without Ansible installed.
	AnsibleBin string
	// Library is the playbook library root. Template resolution v0
	// (pre-manifest, WU-053): template t runs <Library>/<t>.yml.
	Library string
	// Workdir is the root under which each job gets a private 0700 dir for
	// its generated files (extravars now, inventory from WU-051). Removed on
	// terminal state unless KeepWorkdir.
	Workdir string
	// MaxConcurrent caps aggregate running jobs; beyond it jobs hold
	// StateQueued in FIFO order. <=0 → 8. (Per-instance serialization is the
	// instance lock's job — this caps load on the portal host, mini-ADR 1.)
	MaxConcurrent int
	// CancelGrace is the SIGINT→SIGKILL grace on cancel/timeout. <=0 → 10s.
	CancelGrace time.Duration
	// Timeout is the per-job wall-clock cap (the PORTAL_ENGINE_TIMEOUT_CAP
	// ceiling; per-manifest timeouts arrive with WU-053). <=0 → 2h.
	Timeout time.Duration
	// MaxLineBytes caps one log line (longer lines are truncated in place).
	// <=0 → 16 KiB.
	MaxLineBytes int
	// MaxLogBytes caps a job's retained log history (mini-ADR 4): past it the
	// stream carries a single truncation marker and, at job end, the tail.
	// <=0 → 10 MiB.
	MaxLogBytes int64
	// KeepWorkdir keeps per-job dirs after terminal state (debugging).
	KeepWorkdir bool
}

const (
	defaultMaxConcurrent = 8
	defaultCancelGrace   = 10 * time.Second
	defaultTimeout       = 2 * time.Hour
	defaultMaxLineBytes  = 16 * 1024
	defaultMaxLogBytes   = 10 * 1024 * 1024
	// tailKeep is how many past-the-cap lines are retained and flushed at job
	// end — enough for a PLAY RECAP plus the closing error context.
	tailKeep = 100
	// drainGrace bounds the post-exit wait for the log pipe to hit EOF. The
	// group SIGKILL that precedes it makes EOF near-certain; the bound covers
	// a straggler that escaped the group by re-setpgid-ing itself.
	drainGrace = 5 * time.Second
)

// templateNameRE is template-resolution v0's traversal guard: a template is a
// bare name, never a path. (Manifests make resolution declarative in WU-053.)
var templateNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// LocalAdapter is the engine.Adapter that executes ansible-playbook itself,
// in-process, via os/exec (ADR-014, SPEC-050): one supervised child per job,
// fixed argv (never a shell — params reach the child only through files), own
// process group with SIGINT→grace→SIGKILL cancel, Pdeathsig so a dying portal
// never leaks a runner. Job state is in-memory only (mini-ADR 2): a restart
// forgets live jobs, Status answers ErrUnknownJob, and the existing boot
// orphan sweep finalizes their runs — the MockEngine shape, deliberately.
// Safe for concurrent use.
type LocalAdapter struct {
	cfg LocalConfig
	log *slog.Logger

	nonce string // keeps JobIDs from aliasing across restarts (seam contract)
	seq   atomic.Int64

	mu      sync.Mutex
	jobs    map[JobID]*localJob
	queue   []*localJob // FIFO of jobs waiting for a run slot
	running int
}

var _ Adapter = (*LocalAdapter)(nil)

// NewLocalAdapter validates the config shape and returns an adapter. Fail
// closed at wiring time (the config.Validate posture): a missing library or
// an unresolvable binary must stop the boot, not the first launch. The
// workdir root is created here so per-job MkdirAll can never mask a
// misconfigured root.
func NewLocalAdapter(cfg LocalConfig, log *slog.Logger) (*LocalAdapter, error) {
	if cfg.AnsibleBin == "" {
		return nil, errors.New("engine: local adapter needs an ansible-playbook binary (PORTAL_ANSIBLE_BIN)")
	}
	if _, err := exec.LookPath(cfg.AnsibleBin); err != nil {
		return nil, fmt.Errorf("engine: ansible-playbook binary not found: %w", err)
	}
	if cfg.Library == "" {
		return nil, errors.New("engine: local adapter needs a playbook library root (PORTAL_ENGINE_LIBRARY)")
	}
	if info, err := os.Stat(cfg.Library); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("engine: playbook library root %q is not a directory", cfg.Library)
	}
	if cfg.Workdir == "" {
		return nil, errors.New("engine: local adapter needs a job workdir root (PORTAL_ENGINE_WORKDIR)")
	}
	if err := os.MkdirAll(cfg.Workdir, 0o700); err != nil {
		return nil, fmt.Errorf("engine: create workdir root: %w", err)
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = defaultMaxConcurrent
	}
	if cfg.CancelGrace <= 0 {
		cfg.CancelGrace = defaultCancelGrace
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxLineBytes <= 0 {
		cfg.MaxLineBytes = defaultMaxLineBytes
	}
	if cfg.MaxLogBytes <= 0 {
		cfg.MaxLogBytes = defaultMaxLogBytes
	}
	return &LocalAdapter{
		cfg:   cfg,
		log:   log,
		nonce: fmt.Sprintf("%08x", rand.Uint32()),
		jobs:  make(map[JobID]*localJob),
	}, nil
}

type localJob struct {
	mu     sync.Mutex
	status JobStatus
	lines  []LogLine
	subs   []chan LogLine
	done   chan struct{} // closed exactly once, on reaching a terminal state

	playbook string
	params   map[string]string
	workdir  string

	proc      *os.Process // set once spawned
	cancelled bool        // Cancel was requested → terminal state is canceled
	timedOut  bool        // the job timer fired → terminal state is an honest failure
	resultArt *Artifact   // newest DBPORTAL_RESULT parse (scanner-written, under mu)

	// Log caps (mini-ADR 4): past maxLogBytes the history holds one marker
	// and new lines rotate through tail, flushed into history at finish.
	totalBytes int64
	truncated  bool
	tail       []LogLine
	maxBytes   int64
}

// StartJob resolves the template to a playbook file (fail closed — an
// operation with no playbook must never run some other one, the semaphore
// unmapped-tag posture), registers the job queued, and dispatches within the
// concurrency cap. The job outlives ctx: cancelling the request that started
// it must not kill it — that is what Cancel is for.
func (a *LocalAdapter) StartJob(_ context.Context, template string, params map[string]string) (JobID, error) {
	if !templateNameRE.MatchString(template) {
		return "", fmt.Errorf("engine: invalid template name %q", template)
	}
	playbook := filepath.Join(a.cfg.Library, template+".yml")
	if info, err := os.Stat(playbook); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("engine: no playbook for template %q in library %s", template, a.cfg.Library)
	}

	p := make(map[string]string, len(params))
	for k, v := range params {
		p[k] = v
	}

	id := JobID(fmt.Sprintf("local-%s-%d", a.nonce, a.seq.Add(1)))
	j := &localJob{
		status:   JobStatus{ID: id, State: StateQueued},
		done:     make(chan struct{}),
		playbook: playbook,
		params:   p,
		workdir:  filepath.Join(a.cfg.Workdir, string(id)),
		maxBytes: a.cfg.MaxLogBytes,
	}

	a.mu.Lock()
	a.jobs[id] = j
	a.queue = append(a.queue, j)
	a.dispatchLocked()
	a.mu.Unlock()
	return id, nil
}

// dispatchLocked starts queued jobs while run slots are free. Callers hold
// a.mu. FIFO by construction: jobs leave the queue head only.
func (a *LocalAdapter) dispatchLocked() {
	for a.running < a.cfg.MaxConcurrent && len(a.queue) > 0 {
		j := a.queue[0]
		a.queue = a.queue[1:]
		a.running++
		go a.run(j)
	}
}

func (a *LocalAdapter) Status(_ context.Context, id JobID) (JobStatus, error) {
	j, err := a.job(id)
	if err != nil {
		return JobStatus{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, nil
}

// StreamLogs replays history, then follows live output; the channel closes at
// terminal state or ctx cancel (the seam's replay-then-follow contract,
// callable any number of times including after terminal — mock parity).
func (a *LocalAdapter) StreamLogs(ctx context.Context, id JobID) (<-chan LogLine, error) {
	j, err := a.job(id)
	if err != nil {
		return nil, err
	}

	j.mu.Lock()
	// Replay capacity plus live headroom; live sends never block (see emit).
	ch := make(chan LogLine, len(j.lines)+256)
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

// Cancel stops a job. A queued job finishes canceled immediately; a running
// job gets SIGINT on its process group (Ansible's graceful interrupt), then
// SIGKILL after the grace. Terminal jobs are a no-op; the transition is
// asynchronous — observe it via Status or the stream closing (mock parity).
func (a *LocalAdapter) Cancel(_ context.Context, id JobID) error {
	j, err := a.job(id)
	if err != nil {
		return err
	}

	// Pull it from the queue first so dispatch can never start a job whose
	// cancel already returned.
	a.mu.Lock()
	inQueue := false
	for i, q := range a.queue {
		if q == j {
			a.queue = append(a.queue[:i], a.queue[i+1:]...)
			inQueue = true
			break
		}
	}
	a.mu.Unlock()

	j.mu.Lock()
	if j.status.State.Terminal() {
		j.mu.Unlock()
		return nil
	}
	j.cancelled = true
	proc := j.proc
	j.mu.Unlock()

	if inQueue {
		j.finish(StateCanceled, "canceled by operator", nil)
		return nil
	}
	if proc != nil {
		a.interruptGroup(j, proc)
	}
	// proc == nil and not queued: run() is between dequeue and spawn — it
	// re-checks cancelled right before spawning and finishes canceled.
	return nil
}

// interruptGroup delivers the SIGINT→grace→SIGKILL discipline to the job's
// process group (negative pid = the whole group, mini-ADR 1).
func (a *LocalAdapter) interruptGroup(j *localJob, proc *os.Process) {
	_ = syscall.Kill(-proc.Pid, syscall.SIGINT)
	time.AfterFunc(a.cfg.CancelGrace, func() {
		j.mu.Lock()
		terminal := j.status.State.Terminal()
		j.mu.Unlock()
		if !terminal {
			_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
		}
	})
}

func (a *LocalAdapter) job(id JobID) (*localJob, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	j, ok := a.jobs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownJob, id)
	}
	return j, nil
}

// run drives one dequeued job to a terminal state: workdir → extravars file →
// spawn → scan merged output → wait → reap the group → finalize.
func (a *LocalAdapter) run(j *localJob) {
	defer func() {
		a.mu.Lock()
		a.running--
		a.dispatchLocked()
		a.mu.Unlock()
	}()

	if err := os.MkdirAll(j.workdir, 0o700); err != nil {
		j.finish(StateFailed, fmt.Sprintf("create job workdir: %v", err), nil)
		return
	}
	defer a.cleanupWorkdir(j)

	// Params cross to the child ONLY as a file (mini-ADR 6: argv leaks via
	// ps//proc). The forwarded set is the semaphore adapter's fail-closed
	// allowlist verbatim; WU-052 ports it to the dbportal_* namespace.
	argv := []string{j.playbook}
	if ev := extraVars(j.params); ev != "" {
		evPath := filepath.Join(j.workdir, "extravars.json")
		if err := os.WriteFile(evPath, []byte(ev), 0o600); err != nil {
			j.finish(StateFailed, fmt.Sprintf("write extravars file: %v", err), nil)
			return
		}
		argv = append(argv, "--extra-vars", "@"+evPath)
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		j.finish(StateFailed, fmt.Sprintf("create log pipe: %v", err), nil)
		return
	}

	// Fixed argv by construction (mini-ADR 1): a validated library path plus
	// file flags — never a shell, never interpolated user input.
	cmd := exec.Command(a.cfg.AnsibleBin, argv...)
	cmd.Dir = j.workdir
	// stdout+stderr merged into one pipe as one *os.File so cmd.Wait returns
	// on CHILD exit, not on pipe EOF — a straggling grandchild holding the fd
	// must not wedge finalization (mini-ADR 4).
	cmd.Stdout = pw
	cmd.Stderr = pw
	cmd.Env = append(os.Environ(), "ANSIBLE_FORCE_COLOR=0", "PYTHONUNBUFFERED=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Own process group: cancel/timeout signal the whole tree, and the
		// child never receives the portal's own terminal signals.
		Setpgid: true,
		// A dying portal must never leak a runner (mini-ADR 1); the orphan
		// sweep then finalizes the run on the next boot.
		Pdeathsig: syscall.SIGKILL,
	}

	j.mu.Lock()
	if j.cancelled { // Cancel landed between dequeue and spawn
		j.mu.Unlock()
		_ = pr.Close()
		_ = pw.Close()
		j.finish(StateCanceled, "canceled by operator", nil)
		return
	}
	err = cmd.Start()
	if err != nil {
		j.mu.Unlock()
		_ = pr.Close()
		_ = pw.Close()
		j.finish(StateFailed, fmt.Sprintf("spawn %s: %v", a.cfg.AnsibleBin, err), nil)
		return
	}
	j.proc = cmd.Process
	j.status.State = StateRunning
	j.status.Started = time.Now()
	j.mu.Unlock()
	_ = pw.Close() // the child holds the write end now

	// Wall-clock cap: same kill discipline as Cancel, honest failure message.
	timer := time.AfterFunc(a.cfg.Timeout, func() {
		j.mu.Lock()
		if j.status.State.Terminal() {
			j.mu.Unlock()
			return
		}
		j.timedOut = true
		proc := j.proc
		j.mu.Unlock()
		a.interruptGroup(j, proc)
	})
	defer timer.Stop()

	// Scan the merged pipe: every line into the (capped) history, and track
	// the newest DBPORTAL_RESULT sentinel — same regex + field guard as the
	// Semaphore adapter (mini-ADR 3), so playbooks stay engine-portable.
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		defer func() { _ = pr.Close() }()
		r := bufio.NewReaderSize(pr, a.cfg.MaxLineBytes)
		for {
			line, ok := readCappedLine(r)
			if !ok {
				return
			}
			if art, matched := resultFromLine(line); matched {
				j.mu.Lock()
				j.resultArt = art // newest sentinel wins; nil on malformed
				j.mu.Unlock()
			}
			j.emit(line)
		}
	}()

	waitErr := cmd.Wait()
	// Reap the whole group unconditionally: a job's process tree dies with
	// the job, and the pipe is guaranteed to EOF for the drain below.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	select {
	case <-scanDone:
	case <-time.After(drainGrace):
	}
	timer.Stop()

	j.mu.Lock()
	cancelled, timedOut, artifact := j.cancelled, j.timedOut, j.resultArt
	j.mu.Unlock()

	switch {
	case cancelled:
		j.finish(StateCanceled, "canceled by operator", nil)
	case timedOut:
		j.finish(StateFailed, fmt.Sprintf("timed out after %s", a.cfg.Timeout), nil)
	case waitErr == nil:
		j.finish(StateSuccess, "", artifact)
	default:
		j.finish(StateFailed, exitError(cmd, waitErr, j.lastLine()), nil)
	}
}

// cleanupWorkdir removes the job's private dir on terminal state (AC:
// generated files never outlive the job) unless kept for debugging.
func (a *LocalAdapter) cleanupWorkdir(j *localJob) {
	if a.cfg.KeepWorkdir {
		return
	}
	if err := os.RemoveAll(j.workdir); err != nil && a.log != nil {
		a.log.Warn("engine: job workdir cleanup failed", "dir", j.workdir, "err", err.Error())
	}
}

// exitError renders an honest failure reason: the mapped ansible-playbook
// exit code (mini-ADR 3) plus the last output line for context — the full
// PLAY RECAP stays in the log stream for humans.
func exitError(cmd *exec.Cmd, waitErr error, lastLine string) string {
	msg := ""
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			msg = fmt.Sprintf("ansible-playbook killed by signal %s", ws.Signal())
		} else {
			msg = ansibleExitMessage(cmd.ProcessState.ExitCode())
		}
	} else {
		msg = fmt.Sprintf("ansible-playbook: %v", waitErr)
	}
	if lastLine != "" {
		const maxCtx = 200
		if len(lastLine) > maxCtx {
			lastLine = lastLine[:maxCtx] + "…"
		}
		msg += "; last output: " + lastLine
	}
	return msg
}

// ansibleExitMessage maps ansible-playbook's documented exit codes.
func ansibleExitMessage(code int) string {
	desc := map[int]string{
		1:   "generic error",
		2:   "failed task(s)",
		4:   "unreachable host(s)",
		5:   "bad options",
		99:  "user interrupted",
		250: "unexpected error",
	}
	if d, ok := desc[code]; ok {
		return fmt.Sprintf("ansible-playbook exit %d: %s", code, d)
	}
	return fmt.Sprintf("ansible-playbook exit %d", code)
}

// readCappedLine reads one newline-terminated line, truncated in place at the
// reader's buffer size (the remainder of an over-long line is discarded — a
// runaway single line must not grow memory without bound, mini-ADR 4).
// ok=false on EOF with no remaining data.
func readCappedLine(r *bufio.Reader) (string, bool) {
	line, err := r.ReadSlice('\n')
	if len(line) == 0 && err != nil {
		return "", false
	}
	out := string(line)
	if errors.Is(err, bufio.ErrBufferFull) {
		out += "…[line truncated]"
		// Discard the remainder of the physical line.
		for {
			_, derr := r.ReadSlice('\n')
			if !errors.Is(derr, bufio.ErrBufferFull) {
				break
			}
		}
	}
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	if n := len(out); n > 0 && out[n-1] == '\r' {
		out = out[:n-1]
	}
	return out, true
}

// lastLine returns the newest non-empty retained log line.
func (j *localJob) lastLine() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.tail) - 1; i >= 0; i-- {
		if j.tail[i].Line != "" {
			return j.tail[i].Line
		}
	}
	for i := len(j.lines) - 1; i >= 0; i-- {
		if j.lines[i].Line != "" {
			return j.lines[i].Line
		}
	}
	return ""
}

// emit appends a line to history (cap-aware) and fans it out to live
// subscribers. Sends are non-blocking — dropping a live line beats a stuck
// engine (mock precedent); replay always serves the retained history.
func (j *localJob) emit(line string) {
	l := LogLine{TS: time.Now(), Line: line}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.truncated {
		// Past the cap: rotate the tail; it reaches history at finish.
		j.tail = append(j.tail, l)
		if len(j.tail) > tailKeep {
			j.tail = j.tail[1:]
		}
		return
	}
	j.totalBytes += int64(len(line))
	if j.totalBytes > j.maxBytes {
		j.truncated = true
		j.appendLocked(LogLine{TS: l.TS, Line: fmt.Sprintf(
			"[portal: log exceeded %d bytes — truncated; final lines follow at job end]", j.maxBytes)})
		j.tail = append(j.tail, l)
		return
	}
	j.appendLocked(l)
}

// appendLocked stores a line and fans it out. Callers hold j.mu.
func (j *localJob) appendLocked(l LogLine) {
	j.lines = append(j.lines, l)
	for _, ch := range j.subs {
		select {
		case ch <- l:
		default:
		}
	}
}

// finish moves the job to a terminal state exactly once, flushes any
// truncation tail into history, and closes subscriber channels + done.
func (j *localJob) finish(state JobState, errMsg string, artifact *Artifact) {
	j.mu.Lock()
	if j.status.State.Terminal() {
		j.mu.Unlock()
		return
	}
	for _, l := range j.tail {
		j.appendLocked(l)
	}
	j.tail = nil
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
func (j *localJob) removeSub(ch chan LogLine) bool {
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
