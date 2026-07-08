package notify_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/notify"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

// smtpCapture is the message an in-test SMTP server accepted.
type smtpCapture struct {
	from string
	to   []string
	data string
}

// fakeSMTP serves exactly one SMTP session on a random loopback port and
// sends the captured message on the returned channel. Just enough protocol
// for net/smtp: 220 greeting, 250s, 354 for DATA, 221 on QUIT.
func fakeSMTP(t *testing.T) (addr string, got <-chan smtpCapture) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	ch := make(chan smtpCapture, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		var cap smtpCapture
		r := bufio.NewReader(conn)
		say := func(line string) { _, _ = fmt.Fprintf(conn, "%s\r\n", line) }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			cmd := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				cap.from = strings.Trim(line[len("MAIL FROM:"):], "<> ")
				say("250 OK")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				cap.to = append(cap.to, strings.Trim(line[len("RCPT TO:"):], "<> "))
				say("250 OK")
			case cmd == "DATA":
				say("354 go ahead")
				var body strings.Builder
				for {
					dl, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimRight(dl, "\r\n") == "." {
						break
					}
					body.WriteString(dl)
				}
				cap.data = body.String()
				say("250 accepted")
			case cmd == "QUIT":
				say("221 bye")
				ch <- cap
				return
			default:
				say("250 OK")
			}
		}
	}()
	return ln.Addr().String(), ch
}

func strPtr(s string) *string { return &s }

// SPEC-014 behavior 5 (+ mini-ADR 3 negative assertions): the mail names
// who/what/where/status and the run link — and nothing sensitive.
func TestRunEndedSendsPolicyCompliantMail(t *testing.T) {
	addr, got := fakeSMTP(t)
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

	var cap smtpCapture
	select {
	case cap = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP server never completed the session")
	}

	require.Equal(t, "portal@db-portal.local", cap.from)
	require.Equal(t, []string{"dba-a@example.test", "dba-b@example.test"}, cap.to)

	require.Contains(t, cap.data, "Subject: [db-portal] RUN-42 failed — dump on billing-test (test)")
	require.Contains(t, cap.data, "billing-test (test)")
	require.Contains(t, cap.data, "Requested by: local-dev")
	require.Contains(t, cap.data, "http://portal.example:8080/runs/42")

	// The leak-channel guarantees: no reason, no error text, ever.
	require.NotContains(t, cap.data, "CHG-777")
	require.NotContains(t, cap.data, "secret-adjacent")
	require.NotContains(t, cap.data, "pg_dump")
	require.NotContains(t, cap.data, "FATAL")
}

// A dead SMTP endpoint is an error for the caller to log — quickly, not
// after a default OS timeout.
func TestRunEndedDialFailure(t *testing.T) {
	m := &notify.Mailer{Addr: "127.0.0.1:1", From: "a@b", To: []string{"c@d"}, BaseURL: "http://x"}
	err := m.RunEnded(context.Background(), runs.Run{ID: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "notify: dial")
}
