package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newSemaphore points a SemaphoreAdapter at a stub Semaphore server. ProjectID
// 7 lets the tests assert the project-scoped task paths.
func newSemaphore(t *testing.T, h http.Handler) *engine.SemaphoreAdapter {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return engine.NewSemaphoreAdapter(engine.SemaphoreConfig{
		BaseURL:      ts.URL,
		APIToken:     "test-token",
		ProjectID:    7,
		Templates:    map[string]int{"smoke": 3, "dump": 5},
		PollInterval: time.Millisecond,
		HTTPClient:   ts.Client(),
	}, discardLog())
}

// StartJob maps the playbook tag to its template id, posts to the
// project-scoped tasks endpoint with the bearer token, and returns the task
// id as the JobID. An unmapped tag fails closed with NO request sent.
func TestSemaphoreStartJob(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":2147483646,"status":"waiting"}`))
	})
	a := newSemaphore(t, h)

	id, err := a.StartJob(context.Background(), "smoke", nil)
	require.NoError(t, err)
	require.EqualValues(t, "2147483646", id, "the Semaphore task id is the JobID")
	require.Equal(t, "/api/project/7/tasks", gotPath, "task create is project-scoped")
	require.Equal(t, "Bearer test-token", gotAuth)
	require.EqualValues(t, 3, gotBody["template_id"], "the tag maps to its configured template id")
}

func TestSemaphoreStartJobUnmappedTagFailsClosed(t *testing.T) {
	var hit bool
	a := newSemaphore(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))

	_, err := a.StartJob(context.Background(), "restore", nil)
	require.Error(t, err, "an operation with no template mapping must not run a playbook")
	require.False(t, hit, "no task is created for an unmapped tag")
}

// Every Semaphore status maps to the right JobState, and start/end parse.
func TestSemaphoreStatusMapping(t *testing.T) {
	cases := []struct {
		sem   string
		state engine.JobState
	}{
		{"waiting", engine.StateQueued},
		{"starting", engine.StateQueued}, // unrecognized non-terminal → queued, never finalize
		{"running", engine.StateRunning},
		{"success", engine.StateSuccess},
		{"error", engine.StateFailed},
		{"stopped", engine.StateCanceled},
	}
	for _, tc := range cases {
		t.Run(tc.sem, func(t *testing.T) {
			a := newSemaphore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/project/7/tasks/42", r.URL.Path)
				_, _ = fmt.Fprintf(w, `{"id":42,"status":%q,"start":"2026-07-11T19:00:00Z","end":"2026-07-11T19:00:30Z"}`, tc.sem)
			}))
			st, err := a.Status(context.Background(), "42")
			require.NoError(t, err)
			require.Equal(t, tc.state, st.State)
			require.Equal(t, 2026, st.Started.Year(), "start parsed")
			require.Equal(t, 30, st.Finished.Second(), "end parsed")
			if tc.state == engine.StateFailed {
				require.NotEmpty(t, st.Error)
			}
		})
	}
}

// A task Semaphore doesn't know (404, or the observed 400 for a stale id)
// surfaces as ErrUnknownJob from both Status and Cancel — the mock contract
// SweepOrphans relies on.
func TestSemaphoreUnknownJob(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusBadRequest} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			a := newSemaphore(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			_, err := a.Status(context.Background(), "999")
			require.ErrorIs(t, err, engine.ErrUnknownJob)
			require.ErrorIs(t, a.Cancel(context.Background(), "999"), engine.ErrUnknownJob)
		})
	}
}

// StreamLogs replays the output so far then follows new lines, closing when
// the task goes terminal. Semaphore returns the full output every time (no
// cursor), so the adapter must emit only the growing tail — no duplicates.
func TestSemaphoreStreamLogsReplayThenFollow(t *testing.T) {
	var mu sync.Mutex
	statusCalls := 0
	h := http.NewServeMux()
	h.HandleFunc("/api/project/7/tasks/42", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		statusCalls++
		n := statusCalls
		mu.Unlock()
		status := "running"
		if n >= 3 { // terminal on the 3rd status call
			status = "success"
		}
		_, _ = fmt.Fprintf(w, `{"id":42,"status":%q}`, status)
	})
	h.HandleFunc("/api/project/7/tasks/42/output", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n := statusCalls // output grows with each status call: 2, 3, 4 lines
		mu.Unlock()
		count := min(n+1, 4)
		lines := make([]map[string]any, count)
		for i := range lines {
			lines[i] = map[string]any{"time": "2026-07-11T19:00:00Z", "output": fmt.Sprintf("line%d\n", i+1)}
		}
		_ = json.NewEncoder(w).Encode(lines)
	})
	a := newSemaphore(t, h)

	ch, err := a.StreamLogs(context.Background(), "42")
	require.NoError(t, err)
	var got []string
	for l := range ch {
		got = append(got, l.Line)
	}
	require.Equal(t, []string{"line1", "line2", "line3", "line4"}, got,
		"the full tail, in order, each line once (no cursor → dedup by count), then closed on terminal")
}

// StreamLogs on an unknown job is an error, not an empty stream (mock parity).
func TestSemaphoreStreamLogsUnknownJob(t *testing.T) {
	a := newSemaphore(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	_, err := a.StreamLogs(context.Background(), "999")
	require.ErrorIs(t, err, engine.ErrUnknownJob)
}

// Cancel posts to the stop endpoint WITH a JSON body — Semaphore 400s a
// bodyless stop, so the adapter must always send one (even {}).
func TestSemaphoreCancelSendsBody(t *testing.T) {
	var gotPath, gotCT string
	var gotBody string
	a := newSemaphore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, a.Cancel(context.Background(), "42"))
	require.Equal(t, "/api/project/7/tasks/42/stop", gotPath)
	require.Equal(t, "application/json", gotCT)
	require.NotEmpty(t, gotBody, "a bodyless stop is a 400 — the adapter must send {}")
}
