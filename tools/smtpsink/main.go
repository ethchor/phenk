// Command smtpsink is the Phase 0 infrastructure proof: a throwaway SMTP
// server that accepts any recipient and writes each message to a file.
//
// It exists to answer one question before any application code is trusted —
// can real mail from real providers reach this host at all? Many hosting
// providers block inbound port 25 silently, and finding that out after
// building an email service is an expensive way to learn it.
//
// Run it on the host the MX record points at, send mail from Gmail and from
// Outlook, and confirm two .eml files land on disk. Nothing here is used in
// production; the real listener arrives in Phase 2.
//
//	go run ./tools/smtpsink -addr :25 -dir ./inbox
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/emersion/go-smtp"
)

func main() {
	addr := flag.String("addr", ":25", "address to listen on")
	dir := flag.String("dir", "./inbox", "directory to write messages into")
	hostname := flag.String("hostname", "phenk-sink", "hostname announced in the greeting")
	flag.Parse()

	if err := os.MkdirAll(*dir, 0o700); err != nil {
		log.Fatalf("smtpsink: creating %s: %v", *dir, err)
	}

	server := smtp.NewServer(&backend{dir: *dir})
	server.Addr = *addr
	server.Domain = *hostname
	server.ReadTimeout = 60 * time.Second
	server.WriteTimeout = 60 * time.Second
	server.MaxMessageBytes = 25 << 20
	server.MaxRecipients = 50
	server.AllowInsecureAuth = false

	log.Printf("smtpsink: listening on %s, writing to %s", *addr, *dir)
	log.Printf("smtpsink: send mail from Gmail and Outlook, then look for two .eml files")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("smtpsink: %v", err)
	}
}

type backend struct{ dir string }

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remote := "unknown"
	if addr := c.Conn().RemoteAddr(); addr != nil {
		remote = addr.String()
	}
	log.Printf("smtpsink: connection from %s", remote)
	return &session{dir: b.dir, remote: remote}, nil
}

type session struct {
	dir    string
	remote string
	from   string
	rcpts  []string
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from
	log.Printf("smtpsink: MAIL FROM %s", from)
	return nil
}

// Rcpt accepts everything. That is the whole point of a sink and precisely
// what the real server must never do: Phenk rejects unknown recipients at RCPT
// TO rather than accepting and dropping.
func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.rcpts = append(s.rcpts, to)
	log.Printf("smtpsink: RCPT TO %s", to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	name := filepath.Join(s.dir, fmt.Sprintf("%s.eml", time.Now().UTC().Format("20060102T150405.000000000")))
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	defer file.Close()

	n, err := io.Copy(file, r)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	log.Printf("smtpsink: wrote %s (%d bytes) from %s for %v", name, n, s.from, s.rcpts)
	return nil
}

func (s *session) Reset()        { s.from, s.rcpts = "", nil }
func (s *session) Logout() error { return nil }
