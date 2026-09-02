package smtpd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"time"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
)

// Resolution errors. The session maps these onto SMTP replies, and the two
// that mean "there is no such address" map onto identical text so the surface
// leaks nothing about which addresses once existed.
var (
	// ErrUnknownRecipient covers every reason an address cannot receive: it
	// never existed, it expired, it was purged, it is a reserved tombstone, or
	// the name is not one that may be provisioned.
	ErrUnknownRecipient = errors.New("smtpd: no such user here")
	// ErrRelayDenied means we are not the MX for that domain at all.
	ErrRelayDenied = errors.New("smtpd: relay access denied")
	// ErrMailboxFull means the identity is over quota.
	ErrMailboxFull = errors.New("smtpd: mailbox full")
)

// Recipient is the outcome of resolving one RCPT TO.
type Recipient struct {
	LocalPart string
	Domain    core.Domain

	// Identity is nil when the address does not exist yet and the session has
	// been marked to provision it at commit time.
	Identity *core.Identity

	// Provision is set for a valid, unseen name on a public-pool domain.
	Provision bool
}

// Address returns the full recipient address.
func (r *Recipient) Address() string { return r.LocalPart + "@" + r.Domain.Name }

// Message is one accepted SMTP transaction, ready to commit.
type Message struct {
	EnvelopeFrom string
	ClientIP     netip.Addr
	HELO         string
	TLS          bool
	Size         int64

	// Body is the raw message. It is read once, streamed straight into blob
	// storage, and never held in memory.
	Body io.Reader

	Recipients []Recipient
}

// MailReceiver decides who may receive mail and durably commits it.
//
// It is one of the two deliberate single-implementation abstractions in v0.
// Keeping it an interface is what lets the session state machine — where the
// SMTP protocol rules live — be tested against reply codes without a database,
// while the integration tests run the real thing.
type MailReceiver interface {
	// Resolve reports whether a recipient may be accepted at RCPT TO.
	Resolve(ctx context.Context, localPart, domainName string) (*Recipient, error)

	// Commit durably stores the message for every recipient, in one
	// transaction. It returns only once the write is durable, because the
	// caller acknowledges with 250 on the strength of that.
	Commit(ctx context.Context, msg *Message) error
}

// EnqueueFunc schedules follow-up work for a committed delivery, inside the
// transaction that commits it.
//
// Running inside the transaction is the point: a committed message always has a
// parse job and a rolled back one never does. Enqueuing after the commit would
// leave a window where a crash loses the job and the message sits unparsed
// forever with nothing to notice it.
type EnqueueFunc func(ctx context.Context, q pg.Querier, deliveryID core.UUID) error

// storeReceiver is the MailReceiver backed by Postgres and the blob store.
type storeReceiver struct {
	db        *pg.DB
	blobs     blob.Store
	allocator *alloc.Allocator
	enqueue   EnqueueFunc

	domains    *ttlCache[string, core.Domain]
	identities *ttlCache[string, core.Identity]
}

// newStoreReceiver builds the production receiver.
func newStoreReceiver(db *pg.DB, blobs blob.Store, allocator *alloc.Allocator, enqueue EnqueueFunc, cacheTTL time.Duration) *storeReceiver {
	return &storeReceiver{
		db:         db,
		blobs:      blobs,
		allocator:  allocator,
		enqueue:    enqueue,
		domains:    newTTLCache[string, core.Domain](256, cacheTTL),
		identities: newTTLCache[string, core.Identity](4096, cacheTTL),
	}
}

// Resolve implements MailReceiver.
//
// The order matters. The domain decides the policy: on a random-pool domain an
// unknown address is a rejection, and on a public-pool domain it is a name that
// may be provisioned. Getting that the wrong way round would either break the
// shared-inbox feature or let anyone mint addresses on the reputation-sensitive
// pool.
func (r *storeReceiver) Resolve(ctx context.Context, localPart, domainName string) (*Recipient, error) {
	domain, err := r.resolveDomain(ctx, domainName)
	if err != nil {
		return nil, err
	}

	identity, err := r.resolveIdentity(ctx, localPart, domain)
	if err != nil {
		return nil, err
	}

	if identity != nil {
		if !identity.State.AcceptsMail() {
			// Expired, purged and reserved are all reported exactly as an
			// address that never existed.
			return nil, ErrUnknownRecipient
		}
		if identity.QuotaExceeded(0) {
			return nil, ErrMailboxFull
		}
		return &Recipient{LocalPart: localPart, Domain: *domain, Identity: identity}, nil
	}

	if domain.Pool != core.PoolPublic {
		return nil, ErrUnknownRecipient
	}

	// A never-seen name on a public domain: validate it exactly as the HTTP
	// path does, through the one shared validator.
	if err := alloc.ValidateNamed(ctx, r.db, localPart); err != nil {
		if errors.Is(err, core.ErrLocalPartSyntax) ||
			errors.Is(err, core.ErrLocalPartReserved) ||
			errors.Is(err, core.ErrLocalPartBlocked) {
			return nil, ErrUnknownRecipient
		}
		return nil, err
	}
	return &Recipient{LocalPart: localPart, Domain: *domain, Provision: true}, nil
}

func (r *storeReceiver) resolveDomain(ctx context.Context, name string) (*core.Domain, error) {
	if cached, ok := r.domains.Get(name); ok {
		return &cached, nil
	}
	domain, err := pg.DomainByName(ctx, r.db, name)
	if errors.Is(err, pg.ErrNotFound) {
		return nil, ErrRelayDenied
	}
	if err != nil {
		return nil, err
	}
	if domain.State == core.DomainRetired {
		// A retired domain no longer receives mail at all. A burned one still
		// does, for the identities it already hosts.
		return nil, ErrRelayDenied
	}
	r.domains.Put(name, *domain)
	return domain, nil
}

// resolveIdentity returns nil, nil when the address does not exist.
//
// Only accepting identities are cached, and only briefly. A negative answer is
// never cached, because a name that does not exist yet may be provisioned a
// moment later by another sender, and a stale "no" would reject mail for an
// inbox that does exist.
func (r *storeReceiver) resolveIdentity(ctx context.Context, localPart string, domain *core.Domain) (*core.Identity, error) {
	key := localPart + "@" + domain.Name
	if cached, ok := r.identities.Get(key); ok {
		return &cached, nil
	}

	identity, err := pg.IdentityByAddress(ctx, r.db, localPart, domain.ID)
	if errors.Is(err, pg.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if identity.State.AcceptsMail() {
		r.identities.Put(key, *identity)
	}
	return identity, nil
}

// Commit implements MailReceiver.
//
// The blob is written first and the database second, because the blob write is
// the part that cannot participate in a transaction. Everything that can —
// lazy identity creation, the blob row, the sequence reservation, the delivery
// rows and the events — commits together. Only then does the caller
// acknowledge, which is invariant 1.
func (r *storeReceiver) Commit(ctx context.Context, msg *Message) error {
	if len(msg.Recipients) == 0 {
		return errors.New("smtpd: commit with no recipients")
	}

	sha, size, err := r.blobs.Put(ctx, msg.Body)
	if err != nil {
		return fmt.Errorf("smtpd: storing message: %w", err)
	}
	if size != msg.Size {
		// The blob store counted the bytes it actually wrote; trust that over
		// the running count.
		msg.Size = size
	}

	var provisioned []string
	err = r.db.InTx(ctx, func(q pg.Querier) error {
		provisioned = provisioned[:0]

		for i := range msg.Recipients {
			rcpt := &msg.Recipients[i]

			// One reference per delivery, not one per message. Two identities
			// receiving the same bytes share one blob row with a refcount of
			// two, so releasing one of them at purge leaves the other's copy
			// readable.
			blobID, _, err := pg.AcquireBlob(ctx, q, sha, size, r.blobs.Locate(sha))
			if err != nil {
				return err
			}

			identity := rcpt.Identity
			if rcpt.Provision {
				result, err := r.allocator.ProvisionNamed(ctx, q, &rcpt.Domain, rcpt.LocalPart)
				if err != nil {
					return err
				}
				identity = result.Identity
				if result.Created {
					provisioned = append(provisioned, rcpt.Address())
				}
			}

			// Re-read under a row lock. The cached identity may be seconds
			// stale, and this is the point where staleness stops being
			// acceptable: the lock also serializes sequence allocation.
			locked, err := pg.IdentityForUpdate(ctx, q, identity.ID)
			if err != nil {
				return err
			}
			if !locked.State.AcceptsMail() {
				// It expired between RCPT TO and here. Refusing now is
				// correct: accepting and dropping is what invariant 2 forbids.
				return ErrUnknownRecipient
			}
			if locked.QuotaExceeded(size) {
				return ErrMailboxFull
			}

			seq, err := pg.ReserveDeliverySlot(ctx, q, locked.ID, size)
			if err != nil {
				return err
			}
			delivery := &core.Delivery{
				IdentityID:   locked.ID,
				Seq:          seq,
				BlobID:       blobID,
				EnvelopeFrom: msg.EnvelopeFrom,
				ClientIP:     msg.ClientIP,
				HELO:         msg.HELO,
				TLS:          msg.TLS,
				SizeBytes:    size,
				State:        core.DeliveryReceived,
			}
			if err := pg.InsertDelivery(ctx, q, delivery); err != nil {
				return err
			}
			if _, err := pg.AppendEvent(ctx, q, &locked.ID, core.EventMessageReceived, map[string]any{
				"delivery_id": delivery.ID,
				"seq":         seq,
				"size_bytes":  size,
				"from":        msg.EnvelopeFrom,
			}); err != nil {
				return err
			}
			if r.enqueue != nil {
				if err := r.enqueue(ctx, q, delivery.ID); err != nil {
					return fmt.Errorf("smtpd: enqueueing parse for %s: %w", delivery.ID, err)
				}
			}
			rcpt.Identity = locked
		}
		return nil
	})
	if err != nil {
		// The blob bytes stay on disk. They are content-addressed, so a later
		// delivery of the same message reuses them rather than writing again,
		// and deleting them here could race a concurrent session that has
		// already committed a row pointing at the same content.
		return err
	}

	for _, address := range provisioned {
		slog.Info("provisioned named inbox", "address", address, "trigger", "inbound")
	}
	// The cached negative for a freshly provisioned name is never stored, but
	// an existing entry for a name whose usage counters just moved is now
	// stale in a way that matters for quota, so drop it.
	for i := range msg.Recipients {
		r.identities.Forget(msg.Recipients[i].Address())
	}
	return nil
}
