package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/server"
)

// stubReconciler records the job ids the webhook hands it (proving the handler
// passes ONLY the task id, never the payload's claimed status) and can inject
// an error to exercise the log-and-swallow path.
type stubReconciler struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (s *stubReconciler) ReconcileByJobID(_ context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, jobID)
	return s.err
}

func (s *stubReconciler) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

const webhookSecret = "s3cr3t-webhook"

// webhookServer builds a router whose webhook is guarded by `secret`.
func webhookServer(t *testing.T, secret string, rec server.Reconciler) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{
		DB: fakePinger{}, Instances: stubReader{}, Runs: stubRuns{},
		Auth: allowAllAuth{}, Roles: allowAllRoles{},
		Engine: rec, SemaphoreWebhookSecret: secret,
	})
}

// postWebhook posts body to the webhook, optionally presenting a secret header
// (secret == "" sends no header at all).
func postWebhook(t *testing.T, ts *httptest.Server, secret, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/engine/semaphore/webhook", strings.NewReader(body))
	require.NoError(t, err)
	if secret != "" {
		req.Header.Set("X-Portal-Webhook-Secret", secret)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// A valid secret + a task id re-polls that exact run: the handler passes ONLY
// the id to the reconciler (never the payload's status), then answers 204.
func TestWebhookValidSecretReconciles(t *testing.T) {
	rec := &stubReconciler{}
	ts := webhookServer(t, webhookSecret, rec)

	// The payload LIES ("status":"success") — the handler must ignore it and
	// forward only the task id, since poll is the truth (SPEC-033 mini-ADR 2).
	resp := postWebhook(t, ts, webhookSecret, `{"task_id":42,"status":"success"}`)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, []string{"42"}, rec.seen(), "only the task id crosses the seam, never the claimed status")
}

// The task id is tolerated in the shapes Semaphore alerters emit.
func TestWebhookTaskIDShapes(t *testing.T) {
	for name, body := range map[string]string{
		"task_id": `{"task_id":7}`,
		"task.id": `{"task":{"id":7}}`,
		"id":      `{"id":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := &stubReconciler{}
			ts := webhookServer(t, webhookSecret, rec)
			resp := postWebhook(t, ts, webhookSecret, body)
			require.Equal(t, http.StatusNoContent, resp.StatusCode)
			require.Equal(t, []string{"7"}, rec.seen())
		})
	}
}

// Missing / wrong secret → 401 and ZERO reconcile (no state change at all).
func TestWebhookAuthFailsClosed(t *testing.T) {
	cases := map[string]string{
		"missing secret": "",
		"wrong secret":   "not-the-secret",
	}
	for name, presented := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &stubReconciler{}
			ts := webhookServer(t, webhookSecret, rec)
			resp := postWebhook(t, ts, presented, `{"task_id":42}`)
			require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			require.Empty(t, rec.seen(), "a rejected webhook must not touch any run")
		})
	}
}

// An empty CONFIGURED secret disables the accelerator: the route exists but
// 401s everything, even a request that presents an empty secret (poll-only
// mode — the slice-(a) posture, SPEC-033 mini-ADR 5).
func TestWebhookDisabledWhenNoSecretConfigured(t *testing.T) {
	rec := &stubReconciler{}
	ts := webhookServer(t, "", rec)

	require.Equal(t, http.StatusUnauthorized, postWebhook(t, ts, "", `{"task_id":42}`).StatusCode)
	require.Equal(t, http.StatusUnauthorized, postWebhook(t, ts, "anything", `{"task_id":42}`).StatusCode)
	require.Empty(t, rec.seen())
}

// A body with no task id can't name a run → 400, nothing reconciled.
func TestWebhookNoTaskID(t *testing.T) {
	rec := &stubReconciler{}
	ts := webhookServer(t, webhookSecret, rec)
	resp := postWebhook(t, ts, webhookSecret, `{"project_id":1}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Empty(t, rec.seen())
}

func TestWebhookBadJSON(t *testing.T) {
	rec := &stubReconciler{}
	ts := webhookServer(t, webhookSecret, rec)
	resp := postWebhook(t, ts, webhookSecret, `{not json`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Empty(t, rec.seen())
}

// Oversized body → 413 (same cap policy as the other POSTs), after auth.
func TestWebhookBodyTooLarge(t *testing.T) {
	rec := &stubReconciler{}
	ts := webhookServer(t, webhookSecret, rec)
	huge := `{"task_id":42,"pad":"` + strings.Repeat("A", 128<<10) + `"}`
	resp := postWebhook(t, ts, webhookSecret, huge)
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	require.Empty(t, rec.seen())
}

// A reconcile error is swallowed: the handler still answers 2xx so Semaphore
// does not retry — the periodic poll finalizes regardless (poll is the truth).
func TestWebhookReconcileErrorSwallowed(t *testing.T) {
	rec := &stubReconciler{err: context.DeadlineExceeded}
	ts := webhookServer(t, webhookSecret, rec)
	resp := postWebhook(t, ts, webhookSecret, `{"task_id":42}`)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, []string{"42"}, rec.seen(), "the re-poll was attempted; its error did not surface")
}
