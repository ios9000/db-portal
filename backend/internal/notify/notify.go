// Package notify sends run-outcome mail to the DBA list (SPEC-014).
// Dev target is mailpit: plain SMTP, no auth, no TLS. Content policy
// (D7 / ARCHITECTURE §2): who/what/where/status + run link — never the
// operator reason, error text, params or log excerpts.
package notify

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/ios9000/db-portal/backend/internal/runs"
)

// Mailer implements runs.Notifier over SMTP. Zero value is unusable —
// set every field (main wires it from config).
type Mailer struct {
	Addr    string   // SMTP endpoint, host:port
	From    string   // envelope + header sender
	To      []string // the DBA list
	BaseURL string   // portal root, prefixes the /runs/{id} link
}

const dialTimeout = 5 * time.Second

// RunEnded mails one not-success run outcome. The caller (runs.Service)
// treats errors as log-only; a failed send never blocks finalization.
func (m *Mailer) RunEnded(_ context.Context, run runs.Run) error {
	conn, err := net.DialTimeout("tcp", m.Addr, dialTimeout)
	if err != nil {
		return fmt.Errorf("notify: dial %s: %w", m.Addr, err)
	}
	// One deadline for the whole SMTP conversation: a stalled server must
	// not wedge the (tracked) sender goroutine.
	if err := conn.SetDeadline(time.Now().Add(3 * dialTimeout)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("notify: set deadline: %w", err)
	}

	host, _, _ := net.SplitHostPort(m.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("notify: smtp handshake: %w", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Mail(m.From); err != nil {
		return fmt.Errorf("notify: MAIL FROM: %w", err)
	}
	for _, to := range m.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("notify: RCPT TO %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("notify: DATA: %w", err)
	}
	if _, err := w.Write([]byte(m.message(run))); err != nil {
		return fmt.Errorf("notify: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("notify: close body: %w", err)
	}
	return c.Quit()
}

// message renders headers + plain-text body. Every interpolated value is
// portal-controlled (validated inventory names, CHECKed states, catalog
// ids) — no free-form user text enters the mail (SPEC-014 mini-ADR 3).
func (m *Mailer) message(run runs.Run) string {
	subject := fmt.Sprintf("[db-portal] RUN-%d %s — %s on %s (%s)",
		run.ID, run.State, run.Operation, run.Instance, run.Environment)
	link := strings.TrimSuffix(m.BaseURL, "/") + fmt.Sprintf("/runs/%d", run.ID)

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", m.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(m.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")

	fmt.Fprintf(&b, "Run RUN-%d ended with status: %s.\r\n\r\n", run.ID, run.State)
	fmt.Fprintf(&b, "  Operation:    %s\r\n", run.Operation)
	fmt.Fprintf(&b, "  Instance:     %s (%s)\r\n", run.Instance, run.Environment)
	fmt.Fprintf(&b, "  Requested by: %s\r\n\r\n", run.RequestedBy)
	fmt.Fprintf(&b, "View the run: %s\r\n", link)
	return b.String()
}
