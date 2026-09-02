package smtpd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"

	"github.com/emersion/go-smtp"

	"github.com/ethchor/phenk/internal/core"
)

// SMTP replies. The two rejections that mean "there is no such address" share
// one message on purpose: a sender must not be able to tell an address that
// never existed from one that expired, or the SMTP surface becomes an oracle
// for enumerating who used the service.
func replyNoSuchUser() *smtp.SMTPError {
	return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "No such user here"}
}

func replyRelayDenied() *smtp.SMTPError {
	return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "Relay access denied"}
}

func replyMailboxFull() *smtp.SMTPError {
	return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 2, 2}, Message: "Mailbox full"}
}

func replyTooManyRecipients() *smtp.SMTPError {
	return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 5, 3}, Message: "Too many recipients"}
}

func replyProvisionThrottled() *smtp.SMTPError {
	return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 7, 1}, Message: "Too many new addresses, try again later"}
}

func replyTooBig(limit int64) *smtp.SMTPError {
	return &smtp.SMTPError{
		Code:         552,
		EnhancedCode: smtp.EnhancedCode{5, 3, 4},
		Message:      fmt.Sprintf("Message exceeds the %d byte limit", limit),
	}
}

func replyTemporary() *smtp.SMTPError {
	return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "Temporary failure, try again later"}
}

func replyTooManyConnections() *smtp.SMTPError {
	return &smtp.SMTPError{Code: 421, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "Too many connections from your address"}
}

func replySyntax(what string) *smtp.SMTPError {
	return &smtp.SMTPError{Code: 501, EnhancedCode: smtp.EnhancedCode{5, 1, 3}, Message: "Malformed " + what}
}

// session is one SMTP conversation.
type session struct {
	server *Server
	conn   *smtp.Conn
	ctx    context.Context

	clientIP netip.Addr

	from       string
	recipients []Recipient
}

var _ smtp.Session = (*session)(nil)

func (s *session) logger() *slog.Logger {
	return slog.With("client_ip", s.clientIP.String(), "helo", s.conn.Hostname())
}

// Mail begins a transaction. An empty reverse path is legal and common: that is
// how bounces and delivery status notifications identify themselves.
func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.reset()
	if from != "" {
		if _, _, err := core.SplitAddress(from); err != nil {
			return replySyntax("sender address")
		}
	}
	s.from = from
	return nil
}

// Rcpt resolves one recipient and decides, from the domain's pool, whether an
// unknown address is a rejection or a name to provision.
func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	if len(s.recipients) >= s.server.cfg.MaxRecipients {
		return replyTooManyRecipients()
	}

	localPart, domainName, err := core.SplitAddress(to)
	if err != nil {
		return replySyntax("recipient address")
	}

	recipient, err := s.server.receiver.Resolve(s.ctx, localPart, domainName)
	switch {
	case errors.Is(err, ErrUnknownRecipient):
		return replyNoSuchUser()
	case errors.Is(err, ErrRelayDenied):
		return replyRelayDenied()
	case errors.Is(err, ErrMailboxFull):
		return replyMailboxFull()
	case err != nil:
		s.logger().Error("resolving recipient", "rcpt", to, "error", err)
		return replyTemporary()
	}

	if recipient.Provision {
		// Provisioning is the expensive, abusable path: it mints a permanent
		// public address. It is rate limited per source address and capped
		// globally, and both budgets are spent here rather than at commit, so
		// a throttled sender is told to come back rather than being accepted
		// and then failed.
		if !s.server.provisionPerIP.Allow(s.clientIP.String()) {
			s.logger().Warn("provisioning throttled for source", "rcpt", to)
			return replyProvisionThrottled()
		}
		if !s.server.provisionGlobal.Allow("global") {
			s.logger().Warn("global provisioning cap reached", "rcpt", to)
			return replyProvisionThrottled()
		}
	}

	// A repeated RCPT TO for the same address is one delivery, not two.
	for i := range s.recipients {
		if s.recipients[i].Address() == recipient.Address() {
			return nil
		}
	}
	s.recipients = append(s.recipients, *recipient)
	return nil
}

// Data streams the message to a temporary file, then commits it.
//
// Nothing is buffered in memory: a 25MB message from ten simultaneous senders
// would be 250MB of heap, and the size cap has to be enforced while the bytes
// are arriving rather than after they have all been accepted.
func (s *session) Data(r io.Reader) error {
	if s.from == "" && len(s.recipients) == 0 {
		return replySyntax("transaction")
	}
	if len(s.recipients) == 0 {
		return &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 5, 1}, Message: "No valid recipients"}
	}

	limit := s.sizeLimit()
	tmp, err := os.CreateTemp(s.server.cfg.SpoolDir, "phenk-data-*")
	if err != nil {
		s.logger().Error("creating spool file", "error", err)
		return replyTemporary()
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	// One byte past the limit is enough to know the message is too big, and
	// stopping there means a hostile sender cannot make us write more than the
	// limit to disk.
	size, err := io.Copy(tmp, io.LimitReader(r, limit+1))
	if err != nil {
		// go-smtp reports its own global size limit as a 552 through the data
		// reader. Pass that straight through rather than turning a permanent
		// refusal into a temporary one that invites a retry.
		var smtpErr *smtp.SMTPError
		if errors.As(err, &smtpErr) {
			return smtpErr
		}
		s.logger().Warn("reading message data", "error", err)
		return replyTemporary()
	}
	if size > limit {
		s.logger().Warn("message rejected as oversized", "limit", limit)
		return replyTooBig(limit)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		s.logger().Error("rewinding spool file", "error", err)
		return replyTemporary()
	}

	msg := &Message{
		EnvelopeFrom: s.from,
		ClientIP:     s.clientIP,
		HELO:         s.conn.Hostname(),
		// Read now rather than at connection time: STARTTLS is negotiated
		// after the greeting, so a session that upgraded looks cleartext until
		// the moment the data actually arrives.
		TLS:        s.usingTLS(),
		Size:       size,
		Body:       tmp,
		Recipients: s.recipients,
	}

	switch err := s.server.receiver.Commit(s.ctx, msg); {
	case err == nil:
	case errors.Is(err, ErrUnknownRecipient):
		// The address stopped accepting mail between RCPT TO and here.
		return replyNoSuchUser()
	case errors.Is(err, ErrMailboxFull):
		return replyMailboxFull()
	default:
		s.logger().Error("committing message", "error", err)
		return replyTemporary()
	}

	// Only now, with the write durable, is the message acknowledged.
	s.logger().Info("accepted message",
		"from", s.from, "recipients", len(s.recipients), "size_bytes", size, "tls", msg.TLS)
	return nil
}

// sizeLimit is the smallest limit any recipient of this transaction allows.
// Public-pool inboxes take a lower cap, and a message addressed to both pools
// has to satisfy the stricter one, because it is a single set of bytes.
func (s *session) sizeLimit() int64 {
	limit := s.server.cfg.MaxMessageBytes
	for i := range s.recipients {
		if s.recipients[i].Domain.Pool == core.PoolPublic && s.server.cfg.MaxPublicMessageBytes < limit {
			limit = s.server.cfg.MaxPublicMessageBytes
		}
	}
	return limit
}

// Reset abandons the current transaction, as RSET requires.
func (s *session) Reset() { s.reset() }

func (s *session) reset() {
	s.from = ""
	s.recipients = nil
}

// Logout ends the session. The connection slot is released by the connection
// itself, so a client that vanishes without a QUIT still gives it back.
func (s *session) Logout() error { return nil }

// usingTLS reports whether the connection has been upgraded with STARTTLS.
func (s *session) usingTLS() bool {
	_, ok := s.conn.TLSConnectionState()
	return ok
}

// remoteAddr extracts the source address, falling back to the unspecified
// address rather than failing a connection over an unparseable peer.
func remoteAddr(c net.Conn) netip.Addr {
	if c == nil {
		return netip.Addr{}
	}
	if tcp, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		if addr, ok := netip.AddrFromSlice(tcp.IP); ok {
			return addr.Unmap()
		}
	}
	host, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		return netip.Addr{}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}
