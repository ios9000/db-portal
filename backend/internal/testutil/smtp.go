package testutil

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// SMTPCapture is the message an in-test SMTP server accepted.
type SMTPCapture struct {
	From string
	To   []string
	Data string
}

// FakeSMTP serves exactly one SMTP session on a random loopback port and
// sends the captured message on the returned channel. Just enough protocol
// for net/smtp: 220 greeting, 250s, 354 for DATA, 221 on QUIT.
func FakeSMTP(t *testing.T) (addr string, got <-chan SMTPCapture) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	ch := make(chan SMTPCapture, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		var cap SMTPCapture
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
				cap.From = strings.Trim(line[len("MAIL FROM:"):], "<> ")
				say("250 OK")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				cap.To = append(cap.To, strings.Trim(line[len("RCPT TO:"):], "<> "))
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
				cap.Data = body.String()
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
