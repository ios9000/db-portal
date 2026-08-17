package engine_test

// The local adapter's tests run WITHOUT Ansible (SPEC-050 testing strategy):
// TestMain re-invokes this very test binary as a stub ansible-playbook
// (PORTAL_TEST_ANSIBLE_STUB=1) that interprets the "playbook" file as a
// directive script — emit lines, sleep, trap/ignore SIGINT, spawn children,
// exit with a chosen code. Everything the adapter promises (exit map, group
// kill, FIFO queue, caps, workdir lifecycle) is provable in CI this way.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
)

const stubEnvVar = "PORTAL_TEST_ANSIBLE_STUB"

func TestMain(m *testing.M) {
	if os.Getenv(stubEnvVar) == "1" {
		os.Exit(ansibleStubMain())
	}
	// Children spawned by the adapter under test inherit this and become the
	// stub instead of running the test suite. GORACE spares each stub the
	// race runtime's 1s at-exit sleep (it fires on clean exits only —
	// ~1s per successful stub job otherwise).
	_ = os.Setenv(stubEnvVar, "1")
	_ = os.Setenv("GORACE", "atexit_sleep_ms=0")
	os.Exit(m.Run())
}

// ansibleStubMain plays ansible-playbook: argv[1] is the playbook path, whose
// lines are directives executed in order. Unknown directives are ignored so a
// future test can extend the vocabulary without breaking old scripts.
func ansibleStubMain() int {
	if len(os.Args) < 2 {
		fmt.Println("stub: no playbook argument")
		return 250
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Println("stub: read playbook:", err)
		return 250
	}
	for _, raw := range strings.Split(string(data), "\n") {
		directive, arg, _ := strings.Cut(strings.TrimSpace(raw), " ")
		switch directive {
		case "", "#":
		case "echo":
			fmt.Println(arg)
		case "echoerr":
			fmt.Fprintln(os.Stderr, arg)
		case "bigline":
			n, _ := strconv.Atoi(arg)
			fmt.Println(strings.Repeat("x", n))
		case "result":
			fmt.Println("DBPORTAL_RESULT=" + base64.StdEncoding.EncodeToString([]byte(arg)))
		case "result-raw":
			fmt.Println("DBPORTAL_RESULT=" + arg)
		case "sleep":
			d, _ := time.ParseDuration(arg)
			time.Sleep(d)
		case "wait-file":
			for {
				if _, err := os.Stat(arg); err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		case "trap-sigint-exit":
			code, _ := strconv.Atoi(arg)
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, os.Interrupt)
			go func() {
				<-ch
				os.Exit(code)
			}()
		case "ignore-sigint":
			signal.Ignore(os.Interrupt)
		case "spawn-sleeper":
			child := exec.Command("sleep", "300")
			if err := child.Start(); err != nil {
				fmt.Println("stub: spawn-sleeper:", err)
				return 250
			}
			fmt.Printf("STUB_CHILD_PID=%d\n", child.Process.Pid)
		case "spawn-stubborn":
			// A grandchild that survives SIGINT (ignored disposition persists
			// across exec) — only the group SIGKILL can take it down.
			child := exec.Command("sh", "-c", `trap '' INT; exec sleep 300`)
			if err := child.Start(); err != nil {
				fmt.Println("stub: spawn-stubborn:", err)
				return 250
			}
			fmt.Printf("STUB_CHILD_PID=%d\n", child.Process.Pid)
		case "pwd":
			wd, _ := os.Getwd()
			fmt.Println("STUB_CWD=" + wd)
		case "dump-extravars":
			printed := false
			for i, a := range os.Args {
				if a == "--extra-vars" && i+1 < len(os.Args) && strings.HasPrefix(os.Args[i+1], "@") {
					b, _ := os.ReadFile(strings.TrimPrefix(os.Args[i+1], "@"))
					fmt.Println("EXTRAVARS=" + string(b))
					printed = true
				}
			}
			if !printed {
				fmt.Println("EXTRAVARS=none")
			}
		case "dump-inventory":
			// One line so awaitLine can grab it; newlines fold to |.
			printed := false
			for i, a := range os.Args {
				if a == "--inventory" && i+1 < len(os.Args) {
					b, _ := os.ReadFile(os.Args[i+1])
					fmt.Println("INVENTORY=" + strings.ReplaceAll(string(b), "\n", "|"))
					printed = true
				}
			}
			if !printed {
				fmt.Println("INVENTORY=none")
			}
		case "exit":
			code, _ := strconv.Atoi(arg)
			return code
		}
	}
	return 0
}

// writePlaybook drops a stub script as <lib>/<template>.yml.
func writePlaybook(t *testing.T, lib, template, script string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(lib, template+".yml"), []byte(script), 0o600))
}

// newLocalAdapter builds an adapter over the stub binary with snappy test
// timings; mut tweaks the config before construction.
func newLocalAdapter(t *testing.T, lib string, mut func(*engine.LocalConfig)) *engine.LocalAdapter {
	t.Helper()
	cfg := engine.LocalConfig{
		AnsibleBin:  os.Args[0], // this test binary, in stub mode (see TestMain)
		Library:     lib,
		Workdir:     t.TempDir(),
		CancelGrace: 200 * time.Millisecond,
		Timeout:     30 * time.Second,
	}
	if mut != nil {
		mut(&cfg)
	}
	a, err := engine.NewLocalAdapter(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	return a
}

// waitState polls Status until the job reaches want; fails fast on an
// unexpected terminal state.
func waitState(t *testing.T, a *engine.LocalAdapter, id engine.JobID, want engine.JobState) engine.JobStatus {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		st, err := a.Status(context.Background(), id)
		require.NoError(t, err)
		if st.State == want {
			return st
		}
		if st.State.Terminal() || time.Now().After(deadline) {
			t.Fatalf("job %s: state=%s err=%q, want %s", id, st.State, st.Error, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// awaitLine reads the stream until a line with prefix appears, returning the
// remainder of that line.
func awaitLine(t *testing.T, ch <-chan engine.LogLine, prefix string) string {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before a %q line", prefix)
			}
			if strings.HasPrefix(l.Line, prefix) {
				return strings.TrimPrefix(l.Line, prefix)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q line", prefix)
		}
	}
}

// waitProcessGone polls until pid no longer exists (signal 0 errors).
func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d is still alive — leaked child", pid)
}

func drainLines(ch <-chan engine.LogLine) []string {
	var out []string
	for l := range ch {
		out = append(out, l.Line)
	}
	return out
}

func TestLocalHappyPathMergedOutput(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "dump", "echo out-first\nechoerr err-second\necho out-third\nexit 0\n")
	a := newLocalAdapter(t, lib, nil)

	id, err := a.StartJob(context.Background(), "dump", nil)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(id), "local-"), "JobID shape local-<nonce>-<seq>: %s", id)

	st := waitState(t, a, id, engine.StateSuccess)
	require.False(t, st.Started.IsZero())
	require.False(t, st.Finished.IsZero())
	require.Empty(t, st.Error)
	require.Nil(t, st.Artifact, "no result line → no artifact")

	// Replay after terminal: both streams landed in one ordered history.
	ch, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)
	lines := drainLines(ch)
	require.Equal(t, []string{"out-first", "err-second", "out-third"}, lines)

	// Callable again — same replay (seam contract).
	ch2, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, lines, drainLines(ch2))
}

func TestLocalExitCodeMap(t *testing.T) {
	lib := t.TempDir()
	cases := []struct {
		code int
		want string
	}{
		{2, "ansible-playbook exit 2: failed task(s)"},
		{4, "ansible-playbook exit 4: unreachable host(s)"},
		{7, "ansible-playbook exit 7"}, // unmapped code stays honest
	}
	a := newLocalAdapter(t, lib, nil)
	for _, tc := range cases {
		tmpl := fmt.Sprintf("fail%d", tc.code)
		writePlaybook(t, lib, tmpl, fmt.Sprintf("echo boom-%d\nexit %d\n", tc.code, tc.code))
		id, err := a.StartJob(context.Background(), tmpl, nil)
		require.NoError(t, err)
		st := waitState(t, a, id, engine.StateFailed)
		require.Contains(t, st.Error, tc.want)
		require.Contains(t, st.Error, fmt.Sprintf("last output: boom-%d", tc.code),
			"the last output line rides the error for context")
	}
}

func TestLocalResultLineArtifact(t *testing.T) {
	lib := t.TempDir()
	res := map[string]any{
		"name": "widget.dump", "size_bytes": 123, "sha256": "abc123", "location": "s3://b/widget.dump",
	}
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	writePlaybook(t, lib, "dump", "echo working\nresult "+string(raw)+"\nexit 0\n")
	a := newLocalAdapter(t, lib, nil)

	id, err := a.StartJob(context.Background(), "dump", nil)
	require.NoError(t, err)
	st := waitState(t, a, id, engine.StateSuccess)
	require.NotNil(t, st.Artifact)
	require.Equal(t, "widget.dump", st.Artifact.Name)
	require.Equal(t, int64(123), st.Artifact.SizeBytes)
	require.Equal(t, "abc123", st.Artifact.Checksum)
	require.Equal(t, "s3://b/widget.dump", st.Artifact.Location)
}

func TestLocalMalformedResultNil(t *testing.T) {
	lib := t.TempDir()
	scripts := map[string]string{
		"nameonly": `result {"name":"only-a-name"}` + "\nexit 0\n", // WU-040 field guard
		"badb64":   "result-raw AAAAA\nexit 0\n",                   // invalid base64 length
		"badjson":  "result not-json-at-all\nexit 0\n",
	}
	a := newLocalAdapter(t, lib, nil)
	for tmpl, script := range scripts {
		writePlaybook(t, lib, tmpl, script)
		id, err := a.StartJob(context.Background(), tmpl, nil)
		require.NoError(t, err)
		st := waitState(t, a, id, engine.StateSuccess)
		require.Nil(t, st.Artifact, "%s: a malformed result line must register nothing", tmpl)
	}
}

func TestLocalCancelGracefulSigint(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "slow", "trap-sigint-exit 99\necho ready\nsleep 60s\n")
	a := newLocalAdapter(t, lib, nil)

	id, err := a.StartJob(context.Background(), "slow", nil)
	require.NoError(t, err)
	ch, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)
	awaitLine(t, ch, "ready")

	require.NoError(t, a.Cancel(context.Background(), id))
	st := waitState(t, a, id, engine.StateCanceled)
	require.Equal(t, "canceled by operator", st.Error)
	// Cancel on a terminal job is a no-op (mock parity).
	require.NoError(t, a.Cancel(context.Background(), id))
}

func TestLocalCancelKillsProcessGroup(t *testing.T) {
	lib := t.TempDir()
	// Leader ignores SIGINT and its grandchild survives it too — only the
	// post-grace group SIGKILL can end this job. The AC's no-orphan proof.
	writePlaybook(t, lib, "stubborn", "ignore-sigint\nspawn-stubborn\nsleep 300s\n")
	a := newLocalAdapter(t, lib, nil)

	id, err := a.StartJob(context.Background(), "stubborn", nil)
	require.NoError(t, err)
	ch, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)
	childPID, err := strconv.Atoi(awaitLine(t, ch, "STUB_CHILD_PID="))
	require.NoError(t, err)

	require.NoError(t, a.Cancel(context.Background(), id))
	waitState(t, a, id, engine.StateCanceled)
	waitProcessGone(t, childPID)
}

func TestLocalStragglerReapedOnNaturalExit(t *testing.T) {
	lib := t.TempDir()
	// The playbook exits 0 leaving a background child behind: the job's
	// process tree must still die with the job (post-exit group reap).
	writePlaybook(t, lib, "leaky", "spawn-sleeper\nexit 0\n")
	a := newLocalAdapter(t, lib, nil)

	id, err := a.StartJob(context.Background(), "leaky", nil)
	require.NoError(t, err)
	ch, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)
	childPID, err := strconv.Atoi(awaitLine(t, ch, "STUB_CHILD_PID="))
	require.NoError(t, err)

	waitState(t, a, id, engine.StateSuccess)
	waitProcessGone(t, childPID)
}

func TestLocalTimeoutHonestFailure(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "hang", "ignore-sigint\nsleep 60s\n")
	a := newLocalAdapter(t, lib, func(c *engine.LocalConfig) {
		c.Timeout = 300 * time.Millisecond
		c.CancelGrace = 100 * time.Millisecond
	})

	id, err := a.StartJob(context.Background(), "hang", nil)
	require.NoError(t, err)
	st := waitState(t, a, id, engine.StateFailed)
	require.Contains(t, st.Error, "timed out after 300ms")
}

func TestLocalQueueCapFIFO(t *testing.T) {
	lib := t.TempDir()
	release := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		writePlaybook(t, lib, n, "wait-file "+filepath.Join(release, n)+"\nexit 0\n")
	}
	a := newLocalAdapter(t, lib, func(c *engine.LocalConfig) { c.MaxConcurrent = 1 })
	ctx := context.Background()

	idA, err := a.StartJob(ctx, "a", nil)
	require.NoError(t, err)
	idB, err := a.StartJob(ctx, "b", nil)
	require.NoError(t, err)
	idC, err := a.StartJob(ctx, "c", nil)
	require.NoError(t, err)

	waitState(t, a, idA, engine.StateRunning)
	// Above the cap the N+1th (and N+2th) hold queued, Started unstamped.
	time.Sleep(150 * time.Millisecond)
	for _, id := range []engine.JobID{idB, idC} {
		st, err := a.Status(ctx, id)
		require.NoError(t, err)
		require.Equal(t, engine.StateQueued, st.State)
		require.True(t, st.Started.IsZero(), "queued job must not have a start time")
	}

	// Release A → B (not C) takes the slot: FIFO.
	require.NoError(t, os.WriteFile(filepath.Join(release, "a"), nil, 0o600))
	waitState(t, a, idA, engine.StateSuccess)
	waitState(t, a, idB, engine.StateRunning)
	st, err := a.Status(ctx, idC)
	require.NoError(t, err)
	require.Equal(t, engine.StateQueued, st.State, "C must wait its turn behind B")

	require.NoError(t, os.WriteFile(filepath.Join(release, "b"), nil, 0o600))
	waitState(t, a, idB, engine.StateSuccess)
	waitState(t, a, idC, engine.StateRunning)
	require.NoError(t, os.WriteFile(filepath.Join(release, "c"), nil, 0o600))
	waitState(t, a, idC, engine.StateSuccess)
}

func TestLocalCancelQueuedJob(t *testing.T) {
	lib := t.TempDir()
	release := t.TempDir()
	writePlaybook(t, lib, "a", "wait-file "+filepath.Join(release, "a")+"\nexit 0\n")
	writePlaybook(t, lib, "b", "echo never-runs\nexit 0\n")
	a := newLocalAdapter(t, lib, func(c *engine.LocalConfig) { c.MaxConcurrent = 1 })
	ctx := context.Background()

	idA, err := a.StartJob(ctx, "a", nil)
	require.NoError(t, err)
	idB, err := a.StartJob(ctx, "b", nil)
	require.NoError(t, err)
	waitState(t, a, idA, engine.StateRunning)

	require.NoError(t, a.Cancel(ctx, idB))
	st := waitState(t, a, idB, engine.StateCanceled)
	require.True(t, st.Started.IsZero(), "canceled while queued — never ran")

	require.NoError(t, os.WriteFile(filepath.Join(release, "a"), nil, 0o600))
	waitState(t, a, idA, engine.StateSuccess)
	// The canceled job's log history holds nothing from the script.
	ch, err := a.StreamLogs(ctx, idB)
	require.NoError(t, err)
	require.Empty(t, drainLines(ch))
}

func TestLocalWorkdirLifecycleAndExtravars(t *testing.T) {
	lib := t.TempDir()
	release := t.TempDir()
	releaseFile := filepath.Join(release, "go")
	writePlaybook(t, lib, "restore", "pwd\ndump-extravars\nwait-file "+releaseFile+"\nexit 0\n")
	a := newLocalAdapter(t, lib, nil)

	params := map[string]string{
		"instance":      "billing-test", // portal-routing — must NOT cross (allowlist)
		"artifact_name": "widget.dump",
		"checksum":      "abc123",
	}
	id, err := a.StartJob(context.Background(), "restore", params)
	require.NoError(t, err)
	ch, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)

	// The child runs inside its private workdir.
	workdir := awaitLine(t, ch, "STUB_CWD=")
	require.Equal(t, string(id), filepath.Base(workdir))
	info, err := os.Stat(workdir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "job workdir must be private")

	// Params reached the child only through the extravars FILE, allowlisted.
	seen := awaitLine(t, ch, "EXTRAVARS=")
	require.Contains(t, seen, "widget.dump")
	require.Contains(t, seen, "abc123")
	require.NotContains(t, seen, "billing-test", "the routing param must never cross the seam")
	evInfo, err := os.Stat(filepath.Join(workdir, "extravars.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), evInfo.Mode().Perm())

	require.NoError(t, os.WriteFile(releaseFile, nil, 0o600))
	waitState(t, a, id, engine.StateSuccess)
	// Terminal state removes the workdir (and the extravars file with it).
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(workdir); os.IsNotExist(err) {
			break
		}
		require.True(t, time.Now().Before(deadline), "workdir not removed on terminal state")
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLocalUnknownTemplateFailsClosed(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "real", "exit 0\n")
	a := newLocalAdapter(t, lib, nil)
	ctx := context.Background()

	_, err := a.StartJob(ctx, "missing", nil)
	require.ErrorContains(t, err, "no playbook for template")
	for _, evil := range []string{"../evil", "sub/dir", "", ".hidden", "-flag"} {
		_, err := a.StartJob(ctx, evil, nil)
		require.ErrorContains(t, err, "invalid template name", "template %q must be rejected", evil)
	}
}

func TestLocalRestartForgetsJobs(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "quick", "exit 0\n")
	a1 := newLocalAdapter(t, lib, nil)
	id, err := a1.StartJob(context.Background(), "quick", nil)
	require.NoError(t, err)
	waitState(t, a1, id, engine.StateSuccess)

	// A fresh adapter (= a restarted portal) must answer ErrUnknownJob for
	// the old id on every method — the orphan sweep's contract (mini-ADR 2).
	a2 := newLocalAdapter(t, lib, nil)
	_, err = a2.Status(context.Background(), id)
	require.ErrorIs(t, err, engine.ErrUnknownJob)
	_, err = a2.StreamLogs(context.Background(), id)
	require.ErrorIs(t, err, engine.ErrUnknownJob)
	require.ErrorIs(t, a2.Cancel(context.Background(), id), engine.ErrUnknownJob)
}

func TestLocalLogCaps(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "chatty",
		"bigline 500\nbigline 500\nbigline 500\nbigline 500\necho FINAL\nexit 0\n")
	a := newLocalAdapter(t, lib, func(c *engine.LocalConfig) {
		c.MaxLineBytes = 64
		c.MaxLogBytes = 256
	})

	id, err := a.StartJob(context.Background(), "chatty", nil)
	require.NoError(t, err)
	waitState(t, a, id, engine.StateSuccess)
	ch, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)
	lines := drainLines(ch)

	var truncatedLines, markers int
	sawFinal := false
	for _, l := range lines {
		require.Less(t, len(l), 500, "no line may exceed the line cap")
		if strings.Contains(l, "[line truncated]") {
			truncatedLines++
		}
		if strings.Contains(l, "log exceeded 256 bytes") {
			markers++
		}
		if l == "FINAL" {
			sawFinal = true
		}
	}
	require.NotZero(t, truncatedLines, "over-long lines are truncated in place")
	require.Equal(t, 1, markers, "exactly one total-cap truncation marker")
	require.True(t, sawFinal, "the tail past the cap is flushed at job end")
}

func TestNewLocalAdapterFailClosed(t *testing.T) {
	lib := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := engine.LocalConfig{AnsibleBin: os.Args[0], Library: lib, Workdir: t.TempDir()}

	missingBin := base
	missingBin.AnsibleBin = "definitely-not-a-binary-xyz"
	_, err := engine.NewLocalAdapter(missingBin, logger)
	require.ErrorContains(t, err, "not found")

	missingLib := base
	missingLib.Library = filepath.Join(lib, "nope")
	_, err = engine.NewLocalAdapter(missingLib, logger)
	require.ErrorContains(t, err, "not a directory")

	noWorkdir := base
	noWorkdir.Workdir = ""
	_, err = engine.NewLocalAdapter(noWorkdir, logger)
	require.ErrorContains(t, err, "PORTAL_ENGINE_WORKDIR")
}

// fakeInvSource is an in-memory engine.InventorySource holding a "fleet" of
// several instances — the render must expose exactly the target's facts.
type fakeInvSource struct {
	hosts map[string]engine.InventoryHost
}

func (f *fakeInvSource) InventoryHost(_ context.Context, name string) (engine.InventoryHost, error) {
	h, ok := f.hosts[name]
	if !ok {
		return engine.InventoryHost{}, fmt.Errorf("inventory: instance not found: %q", name)
	}
	return h, nil
}

func testFleet() *fakeInvSource {
	return &fakeInvSource{hosts: map[string]engine.InventoryHost{
		"billing-test": {
			Name: "billing-test", Host: "10.0.0.5", Port: 5433,
			Env: "test", Platform: "vm", Cluster: "billing",
		},
		"hr-test": {
			Name: "hr-test", Host: "10.9.9.9", Port: 5432,
			Env: "test", Platform: "vm", Cluster: "hr",
		},
		"crm-dev": {Name: "crm-dev", Env: "dev", Platform: "vm", Cluster: "crm"}, // no tuple
	}}
}

// SPEC-050 mini-ADR 5 (WU-051): the per-job inventory reaches the child via
// --inventory, is 0600 in the job workdir, matches the golden render EXACTLY
// (the internal twin TestRenderInventoryGolden pins the same bytes), and
// never leaks a second instance's address — least privilege by construction.
func TestLocalInventoryRendered(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "dump", "dump-inventory\nexit 0\n")
	var workroot string
	a := newLocalAdapter(t, lib, func(c *engine.LocalConfig) {
		c.Inventory = testFleet()
		c.KeepWorkdir = true
		workroot = c.Workdir
	})

	id, err := a.StartJob(context.Background(), "dump", map[string]string{"instance": "billing-test"})
	require.NoError(t, err)
	ch, err := a.StreamLogs(context.Background(), id)
	require.NoError(t, err)

	// The child received --inventory and could read the file.
	seen := awaitLine(t, ch, "INVENTORY=")
	require.Contains(t, seen, "10.0.0.5")
	waitState(t, a, id, engine.StateSuccess)

	invPath := filepath.Join(workroot, string(id), "inventory.json")
	info, err := os.Stat(invPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "inventory must be private")

	b, err := os.ReadFile(invPath)
	require.NoError(t, err)
	golden := `{
  "target": {
    "hosts": {
      "billing-test": {
        "ansible_host": "10.0.0.5",
        "dbportal_cluster": "billing",
        "dbportal_env": "test",
        "dbportal_instance": "billing-test",
        "dbportal_platform": "vm",
        "dbportal_port": 5433
      }
    }
  }
}
`
	require.Equal(t, golden, string(b))
	require.NotContains(t, string(b), "10.9.9.9", "the fleet must never leak into a job's inventory")
	require.NotContains(t, string(b), "hr-test")
}

// A wired source FAILS CLOSED at StartJob: no recorded tuple or an
// unresolvable instance never becomes a job (mini-ADR 5) — while a job with
// no instance param (adapter-level smoke) still runs, without an inventory.
func TestLocalInventoryFailsClosed(t *testing.T) {
	lib := t.TempDir()
	writePlaybook(t, lib, "dump", "dump-inventory\nexit 0\n")
	a := newLocalAdapter(t, lib, func(c *engine.LocalConfig) { c.Inventory = testFleet() })
	ctx := context.Background()

	_, err := a.StartJob(ctx, "dump", map[string]string{"instance": "crm-dev"})
	require.ErrorContains(t, err, `instance "crm-dev" has no connection info`)

	_, err = a.StartJob(ctx, "dump", map[string]string{"instance": "ghost"})
	require.ErrorContains(t, err, `resolve connection info for "ghost"`)

	id, err := a.StartJob(ctx, "dump", nil)
	require.NoError(t, err)
	ch, err := a.StreamLogs(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "none", awaitLine(t, ch, "INVENTORY="),
		"no instance param → no inventory flag")
	waitState(t, a, id, engine.StateSuccess)
}
