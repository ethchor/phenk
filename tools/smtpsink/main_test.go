package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

// TestSinkWritesAMessage proves the sink works locally before anyone tries it
// against real mail. If this passes and no .eml appears when Gmail sends, the
// problem is the network path, not the program — which is exactly the
// distinction Phase 0 exists to make.
func TestSinkWritesAMessage(t *testing.T) {
	dir := t.TempDir()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	server := smtp.NewServer(&backend{dir: dir})
	server.Domain = "sink.test"
	server.ReadTimeout = 10 * time.Second
	server.WriteTimeout = 10 * time.Second
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client, err := smtp.Dial(listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if err := client.Hello("client.test"); err != nil {
		t.Fatalf("HELO: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("anyone@phenk.test", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}
	w, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	const body = "Subject: hello\r\nFrom: sender@example.com\r\n\r\nreal mail\r\n"
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_ = client.Quit()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files written, want 1", len(entries))
	}
	written, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(written), "real mail") {
		t.Fatalf("the written message is missing its body: %q", written)
	}
	if !strings.HasSuffix(entries[0].Name(), ".eml") {
		t.Fatalf("wrote %q, want a .eml file", entries[0].Name())
	}
}
