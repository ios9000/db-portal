package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/schedule"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// stubSchedules fakes the schedule service so handler tests run without a
// database; the store's real semantics live in internal/schedule tests.
type stubSchedules struct {
	sched   schedule.Schedule
	list    []schedule.Schedule
	err     error
	created *schedule.CreateRequest
	enabled *bool
	deleted *int64
}

func (s stubSchedules) Create(_ context.Context, req schedule.CreateRequest) (schedule.Schedule, error) {
	if s.created != nil {
		*s.created = req
	}
	return s.sched, s.err
}

func (s stubSchedules) List(context.Context) ([]schedule.Schedule, error) {
	return s.list, s.err
}

func (s stubSchedules) SetEnabled(_ context.Context, _ int64, enabled bool) (schedule.Schedule, error) {
	if s.enabled != nil {
		*s.enabled = enabled
	}
	return s.sched, s.err
}

func (s stubSchedules) Delete(_ context.Context, id int64) error {
	if s.deleted != nil {
		*s.deleted = id
	}
	return s.err
}

func sampleSchedule() schedule.Schedule {
	next := time.Date(2026, 7, 11, 2, 30, 0, 0, time.UTC)
	return schedule.Schedule{
		ID: 3, Instance: "billing-test", Env: "test", Operation: "dump",
		CronSpec: "30 2 * * *", Enabled: true, CreatedBy: "dba1",
		CreatedAt: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC), NextFireAt: &next,
	}
}

func schedulesServer(t *testing.T, ss server.ScheduleService) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{
		DB: fakePinger{}, Instances: stubReader{}, Runs: stubRuns{},
		Schedules: ss, Auth: allowAllAuth{}, Roles: allowAllRoles{},
	})
}

func doJSON(t *testing.T, method, url, body string, out any) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
	return resp
}

// The creator becomes the schedule's owner: the session identity rides the
// CreateRequest explicitly (SPEC-022 mini-ADR 9).
func TestCreateSchedulePassesActorAndBody(t *testing.T) {
	var got schedule.CreateRequest
	ts := schedulesServer(t, stubSchedules{sched: sampleSchedule(), created: &got})

	var body schedule.Schedule
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/schedules",
		`{"instance":"billing-test","operation":"dump","cron_spec":"30 2 * * *","reason":"nightly","confirm":"billing-test"}`, &body)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, schedule.CreateRequest{
		Instance: "billing-test", Operation: "dump", CronSpec: "30 2 * * *",
		Reason: "nightly", Confirm: "billing-test", CreatedBy: "local-dev",
	}, got)
	require.Equal(t, int64(3), body.ID)
}

// SPEC-022 API contract: every service refusal maps to its status.
func TestCreateScheduleErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		detail string
	}{
		{"bad spec", schedule.ErrBadSpec, http.StatusBadRequest, "invalid cron spec"},
		{"unknown operation", runs.ErrUnknownOperation, http.StatusBadRequest, "unknown operation"},
		{"unknown instance", runs.ErrUnknownInstance, http.StatusNotFound, "no such instance"},
		{"prod unconfirmed", runs.ErrProdUnconfirmed, http.StatusBadRequest, "typing the instance name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := schedulesServer(t, stubSchedules{err: tc.err})
			var body map[string]string
			resp := doJSON(t, http.MethodPost, ts.URL+"/api/schedules",
				`{"instance":"x","operation":"dump","cron_spec":"@daily"}`, &body)
			require.Equal(t, tc.status, resp.StatusCode)
			require.Contains(t, body["error"], tc.detail)
		})
	}
}

// Same body posture as POST /api/runs (SPEC-021 mini-ADR 7).
func TestCreateScheduleBodyCaps(t *testing.T) {
	ts := schedulesServer(t, stubSchedules{sched: sampleSchedule()})

	var body map[string]string
	huge := `{"instance":"billing-test","operation":"dump","cron_spec":"@daily","reason":"` +
		strings.Repeat("x", 65<<10) + `"}`
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/schedules", huge, &body)
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)

	longReason := `{"instance":"billing-test","operation":"dump","cron_spec":"@daily","reason":"` +
		strings.Repeat("x", 501) + `"}`
	resp = doJSON(t, http.MethodPost, ts.URL+"/api/schedules", longReason, &body)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body["error"], "reason too long")
}

func TestListSchedulesNeverNull(t *testing.T) {
	ts := schedulesServer(t, stubSchedules{})

	var body map[string]json.RawMessage
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/schedules", "", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `[]`, string(body["schedules"]), `"schedules": [], never null`)
}

func TestPatchSchedule(t *testing.T) {
	var enabled bool
	ts := schedulesServer(t, stubSchedules{sched: sampleSchedule(), enabled: &enabled})

	var body schedule.Schedule
	resp := doJSON(t, http.MethodPatch, ts.URL+"/api/schedules/3", `{"enabled":false}`, &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.False(t, enabled)
	require.Equal(t, int64(3), body.ID)

	var errBody map[string]string
	resp = doJSON(t, http.MethodPatch, ts.URL+"/api/schedules/3", `{}`, &errBody)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, errBody["error"], "enabled")

	ts404 := schedulesServer(t, stubSchedules{err: schedule.ErrNotFound})
	resp = doJSON(t, http.MethodPatch, ts404.URL+"/api/schedules/99", `{"enabled":true}`, &errBody)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDeleteSchedule(t *testing.T) {
	var deleted int64
	ts := schedulesServer(t, stubSchedules{deleted: &deleted})

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/schedules/3", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, int64(3), deleted)

	ts404 := schedulesServer(t, stubSchedules{err: schedule.ErrNotFound})
	var errBody map[string]string
	resp = doJSON(t, http.MethodDelete, ts404.URL+"/api/schedules/99", "", &errBody)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
