package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SemaphoreConfig configures a SemaphoreAdapter (SPEC-033). Templates maps a
// playbook tag (the Adapter's `template` arg) to a Semaphore template id;
// ProjectID is the Semaphore project holding them — the task API is
// project-scoped (/api/project/{id}/tasks). HTTPClient is optional (tests
// inject a stub-server client).
type SemaphoreConfig struct {
	BaseURL      string
	APIToken     string
	ProjectID    int
	Templates    map[string]int
	PollInterval time.Duration
	HTTPClient   *http.Client
}

// SemaphoreAdapter is the engine.Adapter over Ansible Semaphore's REST API
// (SPEC-033, ADR-002). It holds NO per-job state: a JobID IS the Semaphore
// task id, and every Status/StreamLogs/Cancel resolves it against Semaphore —
// so a task id survives a portal restart (mini-ADR 1), and an id Semaphore no
// longer knows answers ErrUnknownJob, exactly the mock contract SweepOrphans
// relies on. Poll is the source of truth (mini-ADR 2). Safe for concurrent
// use (stateless over one http.Client).
type SemaphoreAdapter struct {
	cfg  SemaphoreConfig
	http *http.Client
	poll time.Duration
	log  *slog.Logger
}

var _ Adapter = (*SemaphoreAdapter)(nil)

// NewSemaphoreAdapter builds an adapter. A zero PollInterval defaults to 3s;
// a nil HTTPClient gets a 30s-timeout default.
func NewSemaphoreAdapter(cfg SemaphoreConfig, log *slog.Logger) *SemaphoreAdapter {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = 3 * time.Second
	}
	return &SemaphoreAdapter{cfg: cfg, http: hc, poll: poll, log: log}
}

// semTask is the subset of a Semaphore task we read. start/end are RFC 3339
// strings ("" until set); parseTime tolerates absent/empty/garbage as zero.
type semTask struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

type semOutputLine struct {
	Time   time.Time `json:"time"`
	Output string    `json:"output"`
}

// forwardVars is the fail-closed allowlist of engine params that cross to
// Semaphore as Ansible extra-vars (SPEC-036 mini-ADR 1). Only the artifact
// lineage the restore/verify playbooks consume is forwarded — never the
// portal-routing `instance` (the target is selected engine-side by the
// template→Environment binding) or the bookkeeping `artifact_id`. A param
// reaches the real engine only when it is listed here.
var forwardVars = []string{"artifact_name", "checksum"}

// StartJob creates a Semaphore task on the template mapped to the playbook
// tag. An unmapped tag fails closed (mini-ADR 3): an operation with no
// template must never run the wrong playbook. The allowlisted params ride the
// task's `environment` field, which Semaphore forwards to ansible-playbook as
// --extra-vars (SPEC-036) — so the restore/verify playbooks learn which object
// to fetch. A params-free op (dump/safety_dump/smoke) posts no `environment`
// key, an identical body to WU-033/034.
func (a *SemaphoreAdapter) StartJob(ctx context.Context, template string, params map[string]string) (JobID, error) {
	tmplID, ok := a.cfg.Templates[template]
	if !ok {
		return "", fmt.Errorf("engine: no Semaphore template mapped for %q", template)
	}
	body := map[string]any{"template_id": tmplID}
	if env := extraVars(params); env != "" {
		body["environment"] = env
	}
	var task semTask
	if err := a.do(ctx, http.MethodPost, a.tasksPath(), body, &task); err != nil {
		return "", fmt.Errorf("engine: semaphore start %q: %w", template, err)
	}
	return JobID(strconv.FormatInt(task.ID, 10)), nil
}

// extraVars renders the allowlisted, non-empty params as the JSON string
// Semaphore forwards to ansible-playbook as --extra-vars (the task
// `environment` field). Returns "" when none are present, so the task body is
// byte-identical to the params-free WU-033/034 path. The forwarded values are a
// dump name + a hex hash, never secrets.
func extraVars(params map[string]string) string {
	vars := make(map[string]string, len(forwardVars))
	for _, k := range forwardVars {
		if v, ok := params[k]; ok && v != "" {
			vars[k] = v
		}
	}
	if len(vars) == 0 {
		return ""
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return ""
	}
	return string(b)
}

// Status maps the Semaphore task status to a JobState. A task Semaphore
// doesn't know (404, or an observed 400 for a stale id) → ErrUnknownJob.
func (a *SemaphoreAdapter) Status(ctx context.Context, id JobID) (JobStatus, error) {
	var task semTask
	if err := a.do(ctx, http.MethodGet, a.taskPath(id), nil, &task); err != nil {
		return JobStatus{}, asUnknownJob(err, id)
	}
	st := JobStatus{
		ID:       id,
		State:    mapSemState(task.Status),
		Started:  parseTime(task.Start),
		Finished: parseTime(task.End),
	}
	if st.State == StateFailed {
		st.Error = "semaphore task failed"
	}
	// On success, read the machine-readable result line the dump playbook
	// emits and attach the artifact (SPEC-034 mini-ADR 5). Both the watcher
	// and ReconcileByJobID reach finalize through Status, so acceleration and
	// the poll fallback carry the artifact identically. A success that
	// announced no artifact (smoke, or a dump whose line is missing) → nil,
	// and the run still succeeds with no registry row.
	if st.State == StateSuccess {
		st.Artifact = a.fetchArtifact(ctx, id)
	}
	return st, nil
}

// dbportalResultRE matches the single result line the dump playbook emits:
// `DBPORTAL_RESULT=<base64>`. base64's alphabet excludes the surrounding
// quote Ansible's debug callback adds, so the capture stops cleanly at the
// token boundary (SPEC-034 mini-ADR 4).
var dbportalResultRE = regexp.MustCompile(`DBPORTAL_RESULT=([A-Za-z0-9+/=]+)`)

// semDumpResult is the JSON the dump playbook base64-encodes into its result
// line (SPEC-034 mini-ADR 4).
type semDumpResult struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	Location  string `json:"location"`
}

// fetchArtifact reads the task output and parses the result line into an
// Artifact. Best-effort: an output-fetch error, a missing/corrupt line, or a
// success that produced none (smoke) all yield nil — Status still reports
// success, so poll always finalizes; a nil merely means no registry row.
func (a *SemaphoreAdapter) fetchArtifact(ctx context.Context, id JobID) *Artifact {
	var lines []semOutputLine
	if err := a.do(ctx, http.MethodGet, a.taskPath(id)+"/output", nil, &lines); err != nil {
		a.logf("semaphore: could not read task output for artifact", "job", string(id), "err", err.Error())
		return nil
	}
	art := parseResultLine(lines)
	if art == nil {
		// Not necessarily an error — a non-dump playbook produces no line.
		a.logf("semaphore: success task emitted no DBPORTAL_RESULT line (no artifact)", "job", string(id))
	}
	return art
}

// parseResultLine scans task output (newest task last) for the sentinel,
// base64-decodes it, and unmarshals the JSON. A missing sentinel, bad base64,
// bad JSON, or an empty name all yield nil — the adapter never fabricates an
// artifact from a malformed line.
func parseResultLine(lines []semOutputLine) *Artifact {
	for i := len(lines) - 1; i >= 0; i-- {
		m := dbportalResultRE.FindStringSubmatch(lines[i].Output)
		if m == nil {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			return nil
		}
		var r semDumpResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil
		}
		if r.Name == "" {
			return nil
		}
		return &Artifact{Name: r.Name, SizeBytes: r.SizeBytes, Checksum: r.SHA256, Location: r.Location}
	}
	return nil
}

// logf logs at Info if a logger is set (the adapter tolerates a nil logger in
// tests). "no result line" is a normal outcome for non-dump playbooks, so it
// is Info, not Warn — a missing dump artifact surfaces downstream as a
// success with no registry row.
func (a *SemaphoreAdapter) logf(msg string, args ...any) {
	if a.log != nil {
		a.log.Info(msg, args...)
	}
}

// StreamLogs replays the task's output so far, then polls for new lines until
// the task is terminal or ctx is cancelled — the mock's replay-then-follow
// contract (mini-ADR 4). Semaphore's output endpoint always returns the full
// history with no cursor, so we track how many lines we've emitted and send
// only the tail.
func (a *SemaphoreAdapter) StreamLogs(ctx context.Context, id JobID) (<-chan LogLine, error) {
	// Mock parity: an unknown job is an error here, not an empty stream.
	st, err := a.Status(ctx, id)
	if err != nil {
		return nil, err
	}
	ch := make(chan LogLine, 64)
	go a.streamLoop(ctx, id, st.State.Terminal(), ch)
	return ch, nil
}

func (a *SemaphoreAdapter) streamLoop(ctx context.Context, id JobID, terminal bool, ch chan<- LogLine) {
	defer close(ch)
	emitted := 0
	emit := func() error {
		var lines []semOutputLine
		if err := a.do(ctx, http.MethodGet, a.taskPath(id)+"/output", nil, &lines); err != nil {
			return err
		}
		for i := min(emitted, len(lines)); i < len(lines); i++ {
			select {
			case ch <- LogLine{TS: lines[i].Time, Line: strings.TrimRight(lines[i].Output, "\n")}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		emitted = len(lines)
		return nil
	}

	if err := emit(); err != nil { // replay
		return
	}
	for !terminal {
		select {
		case <-ctx.Done():
			return
		case <-time.After(a.poll):
		}
		// Poll status BEFORE output: when a task goes terminal, the following
		// output fetch then captures the complete final log.
		st, err := a.Status(ctx, id)
		if err != nil {
			return
		}
		terminal = st.State.Terminal()
		if err := emit(); err != nil {
			return
		}
	}
}

// Cancel stops a running task. Semaphore's stop endpoint REQUIRES a JSON body
// (a bodyless POST is 400, observed); {} suffices. Stopping a terminal task is
// harmless. An unknown task → ErrUnknownJob.
func (a *SemaphoreAdapter) Cancel(ctx context.Context, id JobID) error {
	err := a.do(ctx, http.MethodPost, a.taskPath(id)+"/stop", map[string]any{}, nil)
	return asUnknownJob(err, id)
}

func (a *SemaphoreAdapter) tasksPath() string {
	return fmt.Sprintf("/api/project/%d/tasks", a.cfg.ProjectID)
}

func (a *SemaphoreAdapter) taskPath(id JobID) string {
	return fmt.Sprintf("/api/project/%d/tasks/%s", a.cfg.ProjectID, id)
}

// do performs one authenticated JSON request. A non-2xx becomes an *httpError
// (callers map task-scoped 400/404 to ErrUnknownJob via asUnknownJob). out may
// be nil (no body decoded); a 204 is never decoded.
func (a *SemaphoreAdapter) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("engine: marshal semaphore request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.cfg.BaseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("engine: semaphore %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &httpError{status: resp.StatusCode, body: strings.TrimSpace(string(b)), method: method, path: path}
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("engine: decode semaphore %s %s: %w", method, path, err)
		}
	}
	return nil
}

// httpError carries a non-2xx Semaphore response.
type httpError struct {
	status       int
	body         string
	method, path string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("semaphore %s %s: %d %s", e.method, e.path, e.status, e.body)
}

// asUnknownJob maps a task-scoped 404 (or an observed 400 for a stale id) to
// ErrUnknownJob, so SweepOrphans and RunDetail treat a task Semaphore forgot
// the same way they treat a mock job lost to a restart.
func asUnknownJob(err error, id JobID) error {
	if err == nil {
		return nil
	}
	var he *httpError
	if errors.As(err, &he) && (he.status == http.StatusNotFound || he.status == http.StatusBadRequest) {
		return fmt.Errorf("%w: %q", ErrUnknownJob, id)
	}
	return err
}

// mapSemState maps Semaphore's task status to a JobState. Non-terminal or
// unrecognized statuses (waiting/starting/…) map to Queued — fail-safe: never
// finalize on a status we don't understand.
func mapSemState(s string) JobState {
	switch strings.ToLower(s) {
	case "success":
		return StateSuccess
	case "error":
		return StateFailed
	case "stopped":
		return StateCanceled
	case "running":
		return StateRunning
	default:
		return StateQueued
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
