package smtpd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

// sendMail delivers one message with a real SMTP client and returns the error
// the server replied with, if any.
func sendMail(t *testing.T, addr, from string, to []string, body string) error {
	t.Helper()

	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()

	if err := client.Hello("sender.test"); err != nil {
		return err
	}
	if err := client.Mail(from, nil); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt, nil); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// smtpCode extracts the numeric reply code from a server error.
func smtpCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var smtpErr *smtp.SMTPError
	if errors.As(err, &smtpErr) {
		return smtpErr.Code
	}
	t.Fatalf("expected an SMTP error, got %T: %v", err, err)
	return 0
}

func smtpMessage(t *testing.T, err error) string {
	t.Helper()
	var smtpErr *smtp.SMTPError
	if errors.As(err, &smtpErr) {
		return smtpErr.Message
	}
	return ""
}

// padding returns roughly n bytes of body text wrapped into realistic lines.
// SMTP limits a line to 998 octets and go-smtp enforces its own line cap, so a
// single enormous line would exercise that rather than the message size cap.
func padding(n int) string {
	const line = "padding padding padding padding padding padding\r\n"
	return strings.Repeat(line, n/len(line)+1)
}

// message builds a small but realistic message body.
func message(subject string) string {
	return strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: recipient@phenk.test",
		"Subject: " + subject,
		"Date: Mon, 02 Jan 2006 15:04:05 -0700",
		"",
		"This is the body of " + subject + ".",
		"",
	}, "\r\n")
}

// rawSession is a hand-driven SMTP conversation, for the cases a well-behaved
// client cannot produce — such as dropping the connection in the middle of
// DATA.
type rawSession struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
}

func dialRaw(t *testing.T, addr string) *rawSession {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	s := &rawSession{t: t, conn: conn, reader: bufio.NewReader(conn)}
	s.expect("220")
	return s
}

func (s *rawSession) send(line string) {
	s.t.Helper()
	if _, err := io.WriteString(s.conn, line+"\r\n"); err != nil {
		s.t.Fatalf("write %q: %v", line, err)
	}
}

// readLine reads one reply line. Replies to several commands can arrive in one
// TCP segment, so this has to be line oriented rather than read oriented.
func (s *rawSession) readLine() string {
	s.t.Helper()
	line, err := s.reader.ReadString('\n')
	if err != nil {
		s.t.Fatalf("read: %v", err)
	}
	return line
}

// expect reads a complete reply, following the continuation lines EHLO
// produces, and asserts its code.
func (s *rawSession) expect(prefix string) string {
	s.t.Helper()
	var reply strings.Builder
	for {
		line := s.readLine()
		reply.WriteString(line)
		trimmed := strings.TrimRight(line, "\r\n")
		// A reply line continues when a hyphen follows the three digit code.
		if len(trimmed) < 4 || trimmed[3] != '-' {
			break
		}
	}
	got := reply.String()
	if !strings.HasPrefix(got, prefix) {
		s.t.Fatalf("got reply %q, want one starting %q", strings.TrimSpace(got), prefix)
	}
	return got
}

func (s *rawSession) command(line, wantPrefix string) string {
	s.t.Helper()
	s.send(line)
	return s.expect(wantPrefix)
}

func (s *rawSession) close() { _ = s.conn.Close() }
