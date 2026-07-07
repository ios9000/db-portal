package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// stubRuns fakes internal/runs for handler tests (the real Service is
// covered by DB-backed tests in internal/runs).
type stubRuns struct {
	run       runs.Run
	list      []runs.Run
	err       error
	started   *startRunCall
	logs      []engine.LogLine
	logsErr   error
	cancelErr error
	canceled  *int64
}

type startRunCall struct {
	instance, operation, reason string
	params                      map[string]string
}

func (s stubRuns) Start(_ context.Context, instance, operation, reason string, params map[string]string) (runs.Run, error) {
	if s.started != nil {
		*s.started = startRunCall{instance, operation, reason, params}
	}
	return s.run, s.err
}

func (s stubRuns) Get(context.Context, int64) (runs.Run, error) {
	if s.err != nil {
		return runs.Run{}, s.err
	}
	return s.run, nil
}

func (s stubRuns) List(context.Context, string) ([]runs.Run, error) {
	return s.list, s.err
}

// StreamLogs replays the stubbed lines and closes — the finished-job shape;
// live-follow behavior belongs to the engine/service tests.
func (s stubRuns) StreamLogs(context.Context, int64) (<-chan engine.LogLine, error) {
	if s.logsErr != nil {
		return nil, s.logsErr
	}
	ch := make(chan engine.LogLine, len(s.logs))
	for _, l := range s.logs {
		ch <- l
	}
	close(ch)
	return ch, nil
}

func (s stubRuns) Cancel(_ context.Context, id int64) error {
	if s.canceled != nil {
		*s.canceled = id
	}
	return s.cancelErr
}

func sampleRun() runs.Run {
	job := "mock-nonprod-1"
	return runs.Run{
		ID: 7, Instance: "billing-test", Environment: "test", Operation: "dump",
		State: "queued", JobID: &job, SubmittedAt: time.Now().UTC(),
	}
}

func runsServer(t *testing.T, rs server.RunService) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{DB: fakePinger{}, Instances: stubReader{}, Runs: rs})
}

func TestListOperationsServesCatalog(t *testing.T) {
	ts := runsServer(t, stubRuns{})

	var body struct {
		Operations []map[string]any `json:"operations"`
	}
	resp := apiGet(t, ts, "/api/operations", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, body.Operations, 1)
	op := body.Operations[0]
	require.Equal(t, "dump", op["id"])
	require.Equal(t, "Backup", op["label"])
	require.NotEmpty(t, op["description"])
	require.NotEmpty(t, op["duration_hint"])
	// Engine internals stay server-side.
	require.NotContains(t, op, "template")
	require.NotContains(t, op, "playbook_tag")
	require.NotContains(t, op, "Template")
}

func postRuns(t *testing.T, ts *httptest.Server, body string, out any) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/runs", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	return resp
}

func TestStartRun(t *testing.T) {
	called := &startRunCall{}
	ts := runsServer(t, stubRuns{run: sampleRun(), started: called})

	var body map[string]any
	resp := postRuns(t, ts, `{"instance":"billing-test","operation":"dump","reason":"CHG-1"}`, &body)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.EqualValues(t, 7, body["id"])
	require.Equal(t, "queued", body["state"])
	require.Equal(t, "billing-test", body["instance"])

	require.Equal(t, "billing-test", called.instance)
	require.Equal(t, "dump", called.operation)
	require.Equal(t, "CHG-1", called.reason)
	require.Nil(t, called.params, "no client engine params may cross the API (SPEC-012)")
}

func TestStartRunErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		err        error
		wantStatus int
		wantError  string
	}{
		{"bad json", `{`, nil, http.StatusBadRequest, "bad JSON body"},
		{"unknown operation", `{"instance":"a","operation":"x"}`, runs.ErrUnknownOperation, http.StatusBadRequest, "unknown operation"},
		{"unknown instance", `{"instance":"nope","operation":"dump"}`, runs.ErrUnknownInstance, http.StatusNotFound, "no such instance"},
		{"engine refused", `{"instance":"a","operation":"dump"}`, runs.ErrEngine, http.StatusBadGateway, "engine refused"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := runsServer(t, stubRuns{err: tc.err})
			var body map[string]string
			resp := postRuns(t, ts, tc.body, &body)
			require.Equal(t, tc.wantStatus, resp.StatusCode)
			require.Contains(t, body["error"], tc.wantError)
		})
	}
}

func TestListRuns(t *testing.T) {
	ts := runsServer(t, stubRuns{list: []runs.Run{sampleRun()}})

	var body struct {
		Runs []map[string]any `json:"runs"`
	}
	resp := apiGet(t, ts, "/api/runs", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, body.Runs, 1)
	require.Equal(t, "billing-test", body.Runs[0]["instance"])
	// Artifact serializes as explicit null until one exists.
	require.Contains(t, body.Runs[0], "artifact")
	require.Nil(t, body.Runs[0]["artifact"])
}

func TestListRunsEmptyIsJSONArray(t *testing.T) {
	ts := runsServer(t, stubRuns{})

	var body struct {
		Runs []map[string]any `json:"runs"`
	}
	resp := apiGet(t, ts, "/api/runs", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotNil(t, body.Runs, `"runs" must be [], not null`)
}

func TestGetRun(t *testing.T) {
	ts := runsServer(t, stubRuns{run: sampleRun()})

	var body map[string]any
	resp := apiGet(t, ts, "/api/runs/7", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 7, body["id"])
}

func TestGetRunNotFound(t *testing.T) {
	for _, path := range []string{"/api/runs/999", "/api/runs/not-a-number"} {
		ts := runsServer(t, stubRuns{err: runs.ErrNotFound})
		var body map[string]string
		resp := apiGet(t, ts, path, &body)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, path)
		require.Contains(t, body["error"], "no such run")
	}
}

// SPEC-013 behavior 3: log events stream in order, then exactly one `end`
// event carrying the run state, then the connection closes.
func TestStreamRunLogsSSE(t *testing.T) {
	run := sampleRun()
	run.State = "success"
	ts := runsServer(t, stubRuns{
		run: run,
		logs: []engine.LogLine{
			{TS: time.Now(), Line: "PLAY [dump] start"},
			{TS: time.Now(), Line: "TASK [dump : run pg_dump] ok"},
		},
	})

	resp, err := http.Get(ts.URL + "/api/runs/7/logs")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	raw, err := io.ReadAll(resp.Body) // the end event closes the stream
	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, `event: log`)
	require.Contains(t, body, `"line":"PLAY [dump] start"`)
	require.Contains(t, body, `"line":"TASK [dump : run pg_dump] ok"`)
	require.Contains(t, body, "event: end\ndata: {\"state\":\"success\"}")
	require.Less(t, strings.LastIndex(body, "event: log"), strings.Index(body, "event: end"),
		"all log events precede the end event")
	require.Equal(t, 1, strings.Count(body, "event: end"))
}

// SPEC-013 behavior 4, HTTP mapping: 404 unknown run, 410 logs gone.
func TestStreamRunLogsErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"unknown run", runs.ErrNotFound, http.StatusNotFound},
		{"logs gone", runs.ErrNoLogs, http.StatusGone},
		{"backend broken", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := runsServer(t, stubRuns{logsErr: tc.err})
			var body map[string]string
			resp := apiGet(t, ts, "/api/runs/7/logs", &body)
			require.Equal(t, tc.wantStatus, resp.StatusCode)
			require.NotEmpty(t, body["error"])
		})
	}
}

func postCancel(t *testing.T, ts *httptest.Server, path string, out any) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	return resp
}

// SPEC-013: cancel is accepted asynchronously — 202, outcome via polling.
func TestCancelRun(t *testing.T) {
	var canceled int64
	ts := runsServer(t, stubRuns{canceled: &canceled})

	var body map[string]string
	resp := postCancel(t, ts, "/api/runs/7/cancel", &body)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Equal(t, "canceling", body["status"])
	require.EqualValues(t, 7, canceled)
}

func TestCancelRunErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"unknown run", runs.ErrNotFound, http.StatusNotFound},
		{"already terminal", runs.ErrNotCancelable, http.StatusConflict},
		{"engine refused", runs.ErrEngine, http.StatusBadGateway},
		{"backend broken", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := runsServer(t, stubRuns{cancelErr: tc.err})
			var body map[string]string
			resp := postCancel(t, ts, "/api/runs/7/cancel", &body)
			require.Equal(t, tc.wantStatus, resp.StatusCode)
			require.NotEmpty(t, body["error"])
		})
	}
}
