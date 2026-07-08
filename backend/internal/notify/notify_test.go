package notify_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/notify"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

func strPtr(s string) *string { return &s }

// SPEC-014 behavior 5 (+ mini-ADR 3 negative assertions): the mail names
// who/what/where/status and the run link — and nothing sensitive.
func TestRunEndedSendsPolicyCompliantMail(t *testing.T) {
	addr, got := testutil.FakeSMTP(t)
	m := &notify.Mailer{
		Addr:    addr,
		From:    "portal@db-portal.local",
		To:      []string{"dba-a@example.test", "dba-b@example.test"},
		BaseURL: "http://portal.example:8080/",
	}

	run := runs.Run{
		ID:          42,
		Instance:    "billing-test",
		Environment: "test",
		Operation:   "dump",
		State:       "failed",
		RequestedBy: "local-dev",
		Reason:      strPtr("CHG-777 secret-adjacent ticket text"),
		Error:       strPtr("pg_dump: connection to server failed: FATAL: password"),
	}
	require.NoError(t, m.RunEnded(context.Background(), run))

	var cap testutil.SMTPCapture
	select {
	case cap = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP server never completed the session")
	}

	require.Equal(t, "portal@db-portal.local", cap.From)
	require.Equal(t, []string{"dba-a@example.test", "dba-b@example.test"}, cap.To)

	require.Contains(t, cap.Data, "Subject: [db-portal] RUN-42 failed — dump on billing-test (test)")
	require.Contains(t, cap.Data, "billing-test (test)")
	require.Contains(t, cap.Data, "Requested by: local-dev")
	require.Contains(t, cap.Data, "http://portal.example:8080/runs/42")

	// The leak-channel guarantees: no reason, no error text, ever.
	require.NotContains(t, cap.Data, "CHG-777")
	require.NotContains(t, cap.Data, "secret-adjacent")
	require.NotContains(t, cap.Data, "pg_dump")
	require.NotContains(t, cap.Data, "FATAL")
}

// A dead SMTP endpoint is an error for the caller to log — quickly, not
// after a default OS timeout.
func TestRunEndedDialFailure(t *testing.T) {
	m := &notify.Mailer{Addr: "127.0.0.1:1", From: "a@b", To: []string{"c@d"}, BaseURL: "http://x"}
	err := m.RunEnded(context.Background(), runs.Run{ID: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "notify: dial")
}
