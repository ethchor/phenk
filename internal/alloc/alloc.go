// Package alloc allocates addresses.
//
// It is the single place that decides what an address looks like, which domain
// it lands on, and whether a name may be provisioned at all. The SMTP and HTTP
// paths both provision named inboxes, and they share the validator here rather
// than each carrying their own copy of §6.5.
package alloc

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/pg"
)

// Errors returned by allocation.
var (
	// ErrNoDomains means the pool has no active domain. Every domain in it is
	// fresh, burned or retired, so nothing can be allocated until an operator
	// promotes one.
	ErrNoDomains = errors.New("alloc: no active domain in pool")
	// ErrExhausted means repeated address generation kept colliding, which in
	// practice means something is very wrong rather than unlucky.
	ErrExhausted = errors.New("alloc: could not find a free address")
)

// allocationAttempts bounds the collision retry. At 50 bits of entropy a single
// collision is already surprising; five in a row is a bug, not bad luck.
const allocationAttempts = 5

// Allocator mints identities. It is safe for concurrent use.
type Allocator struct {
	keyring    *crypto.Keyring
	defaultTTL time.Duration
	maxTTL     time.Duration
	namedHours int
}

// Options configures an Allocator.
type Options struct {
	DefaultTTL     time.Duration
	MaxTTL         time.Duration
	NamedRetention time.Duration
}

// New returns an Allocator.
func New(keyring *crypto.Keyring, opts Options) *Allocator {
	if opts.DefaultTTL <= 0 {
		opts.DefaultTTL = time.Hour
	}
	if opts.MaxTTL <= 0 {
		opts.MaxTTL = 24 * time.Hour
	}
	if opts.NamedRetention <= 0 {
		opts.NamedRetention = time.Duration(core.DefaultNamedRetentionHours) * time.Hour
	}
	return &Allocator{
		keyring:    keyring,
		defaultTTL: opts.DefaultTTL,
		maxTTL:     opts.MaxTTL,
		namedHours: int(opts.NamedRetention / time.Hour),
	}
}

// Result is a freshly allocated or resolved identity together with the domain
// it lives on.
type Result struct {
	Identity *core.Identity
	Domain   core.Domain
	// Created is false when a named inbox already existed and was merely
	// resolved. It decides whether identity.created is emitted.
	Created bool
}

// Address returns the full address.
func (r *Result) Address() string { return r.Identity.LocalPart + "@" + r.Domain.Name }

// GenerateLocalPart returns a random address local part: ten Crockford base32
// characters, which is 50 bits of entropy with no character that can be misread
// when someone copies an address off a screen.
//
// The alphabet is exactly 32 characters, so reducing a random byte modulo 32 is
// uniform and needs no rejection sampling.
func GenerateLocalPart() string {
	buf := make([]byte, core.RandomLocalPartLen)
	if _, err := rand.Read(buf); err != nil {
		panic("alloc: random source unavailable: " + err.Error())
	}
	for i, b := range buf {
		buf[i] = core.RandomAlphabet[int(b)%len(core.RandomAlphabet)]
	}
	return string(buf)
}

// SelectDomain picks an active domain from a pool at random, spreading new
// addresses across the pool rather than concentrating reputation damage on
// whichever domain sorts first.
func SelectDomain(ctx context.Context, q pg.Querier, pool core.Pool) (*core.Domain, error) {
	domains, err := pg.AllocatableDomains(ctx, q, pool)
	if err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoDomains, pool)
	}
	return &domains[pickIndex(len(domains))], nil
}

// AllocateRandom mints a session-owned identity on a random-pool domain.
//
// Each attempt is its own transaction because a unique violation aborts the
// transaction it happens in, so a collision has to be retried with a clean one.
func (a *Allocator) AllocateRandom(ctx context.Context, db *pg.DB, session string, ttl time.Duration) (*Result, error) {
	if ttl <= 0 {
		ttl = a.defaultTTL
	}
	if ttl > a.maxTTL {
		ttl = a.maxTTL
	}

	var lastErr error
	for attempt := 0; attempt < allocationAttempts; attempt++ {
		var result *Result
		err := db.InTx(ctx, func(q pg.Querier) error {
			domain, err := SelectDomain(ctx, q, core.PoolRandom)
			if err != nil {
				return err
			}

			id := core.NewUUID()
			_, wrapped, err := a.keyring.NewDataKey(id)
			if err != nil {
				return err
			}
			expires := time.Now().Add(ttl).UTC()
			identity := &core.Identity{
				ID:             id,
				LocalPart:      GenerateLocalPart(),
				DomainID:       domain.ID,
				Kind:           core.KindRandom,
				State:          core.IdentityActive,
				OwnerSession:   session,
				WrappedDataKey: wrapped,
				QuotaMessages:  core.DefaultRandomQuotaMessages,
				QuotaBytes:     core.DefaultRandomQuotaBytes,
				ExpiresAt:      &expires,
			}
			if err := pg.CreateIdentity(ctx, q, identity); err != nil {
				return err
			}
			if _, err := pg.AppendEvent(ctx, q, &identity.ID, core.EventIdentityCreated, map[string]any{
				"address":    identity.LocalPart + "@" + domain.Name,
				"kind":       string(core.KindRandom),
				"expires_at": expires,
			}); err != nil {
				return err
			}
			result = &Result{Identity: identity, Domain: *domain, Created: true}
			return nil
		})
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, pg.ErrConflict) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w after %d attempts: %v", ErrExhausted, allocationAttempts, lastErr)
}

// ValidateNamed applies the whole of §6.5 to a name: the shared syntactic
// grammar, the generated-address shape check, and the denylist. Both the SMTP
// RCPT TO path and POST /v1/named call this, which is the point of it living
// here.
//
// The local part must already be normalized.
func ValidateNamed(ctx context.Context, q pg.Querier, localPart string) error {
	if err := core.ValidateNamedLocalPart(localPart); err != nil {
		return err
	}
	blocked, err := pg.IsLocalPartBlocked(ctx, q, localPart)
	if err != nil {
		return err
	}
	if blocked {
		return fmt.Errorf("%w: %s", core.ErrLocalPartBlocked, localPart)
	}
	return nil
}

// ProvisionNamed resolves a named inbox, creating it if this is the first time
// anyone has asked for it.
//
// It takes a Querier rather than a pool because the SMTP path calls it inside
// the delivery commit transaction: the identity and the delivery that caused it
// to exist have to land together or not at all. Two senders reaching the same
// new name at once both succeed and both end up with the same identity, and
// exactly one of them is told it was the creator.
func (a *Allocator) ProvisionNamed(ctx context.Context, q pg.Querier, domain *core.Domain, localPart string) (*Result, error) {
	if domain.Pool != core.PoolPublic {
		return nil, fmt.Errorf("%w: %s is a %s-pool domain", core.ErrKindPoolMismatch, domain.Name, domain.Pool)
	}
	if err := ValidateNamed(ctx, q, localPart); err != nil {
		return nil, err
	}

	id := core.NewUUID()
	_, wrapped, err := a.keyring.NewDataKey(id)
	if err != nil {
		return nil, err
	}
	retention := a.namedHours
	candidate := &core.Identity{
		ID:             id,
		LocalPart:      localPart,
		DomainID:       domain.ID,
		Kind:           core.KindNamed,
		State:          core.IdentityActive,
		WrappedDataKey: wrapped,
		RetentionHours: &retention,
		QuotaMessages:  core.DefaultNamedQuotaMessages,
		QuotaBytes:     core.DefaultNamedQuotaBytes,
	}

	stored, created, err := pg.CreateIdentityIfAbsent(ctx, q, candidate)
	if err != nil {
		return nil, err
	}
	if created {
		if _, err := pg.AppendEvent(ctx, q, &stored.ID, core.EventIdentityCreated, map[string]any{
			"address": stored.LocalPart + "@" + domain.Name,
			"kind":    string(core.KindNamed),
		}); err != nil {
			return nil, err
		}
	}
	return &Result{Identity: stored, Domain: *domain, Created: created}, nil
}

// ResolveOrCreateNamed is the UI entry point: a user types a name into the
// inbox switcher and gets a live inbox back.
//
// A name resolves to exactly one inbox. An existing one is returned whatever
// domain it happens to live on; a new one lands on a domain chosen by hashing
// the name, so the same name always produces the same address and a user who
// comes back tomorrow reaches the messages they were expecting.
func (a *Allocator) ResolveOrCreateNamed(ctx context.Context, db *pg.DB, localPart string) (*Result, error) {
	localPart = core.NormalizeLocalPart(localPart)
	if err := core.ValidateNamedLocalPart(localPart); err != nil {
		return nil, err
	}

	var result *Result
	err := db.InTx(ctx, func(q pg.Querier) error {
		identity, domain, err := pg.NamedIdentityByLocalPart(ctx, q, localPart)
		switch {
		case err == nil:
			result = &Result{Identity: identity, Domain: *domain, Created: false}
			return nil
		case !errors.Is(err, pg.ErrNotFound):
			return err
		}

		domain, err = SelectNamedDomain(ctx, q, localPart)
		if err != nil {
			return err
		}
		result, err = a.ProvisionNamed(ctx, q, domain, localPart)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SelectNamedDomain picks the public-pool domain a name belongs on, by hashing
// the name across the pool. Unlike random allocation this must be
// deterministic: an address a user pastes into a signup form has to still be
// theirs on the next visit.
//
// Changing the pool re-hashes names that have never been used, which is
// harmless; names already in use keep their domain because the existing-inbox
// lookup runs first.
func SelectNamedDomain(ctx context.Context, q pg.Querier, localPart string) (*core.Domain, error) {
	domains, err := pg.AllocatableDomains(ctx, q, core.PoolPublic)
	if err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoDomains, core.PoolPublic)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(localPart))
	return &domains[h.Sum64()%uint64(len(domains))], nil
}

// pickIndex returns a uniform index below n using the system random source.
func pickIndex(n int) int {
	if n <= 1 {
		return 0
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("alloc: random source unavailable: " + err.Error())
	}
	v := uint64(0)
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return int(v % uint64(n))
}
