package api

import (
	"errors"
	"net/http"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// WaitForNamedMessages implements apigen.ServerInterface.
//
// This is the call an agent makes: pick a fresh name, put it in a form, wait
// on it. Waiting on a name nobody has used yet opens it first, exactly as
// POST /v1/named would and under the same limit. Otherwise the first wait on a
// fresh name would 404 until the email arrived and provisioned the inbox —
// and that email is the thing being waited for.
func (s *Server) WaitForNamedMessages(w http.ResponseWriter, r *http.Request, address string, params apigen.WaitForNamedMessagesParams) {
	identity, ok := s.namedIdentityOrOpen(w, r, address)
	if !ok {
		return
	}
	s.wait(w, r, identity, since(params.Since), waitTimeout(params.Timeout, s.cfg.MaxWaitTimeout))
}

// GetLatestNamedMessage implements apigen.ServerInterface.
func (s *Server) GetLatestNamedMessage(w http.ResponseWriter, r *http.Request, address string) {
	identity, ok := s.namedIdentity(w, r, address)
	if !ok {
		return
	}
	if len(identity.WrappedDataKey) == 0 {
		notFound(w)
		return
	}

	delivery, err := pg.LatestDelivery(r.Context(), s.db, identity.ID)
	if errors.Is(err, pg.ErrNotFound) {
		// An inbox with no mail yet. Indistinguishable from no inbox at
		// all, which is fine: either way there is nothing to read.
		notFound(w)
		return
	}
	if err != nil {
		internalError(w, r, "loading the latest message", err)
		return
	}
	s.writeMessage(w, r, delivery, identity)
}

// namedIdentityOrOpen resolves a public inbox, opening it first when the name
// has never been used.
//
// With a full address, the inbox is opened on that domain, the same way the
// SMTP path opens one when mail arrives for it — and only if the domain is
// currently handing out addresses, for the same reason SMTP refuses a new name
// on a domain that is warming up or burned.
func (s *Server) namedIdentityOrOpen(w http.ResponseWriter, r *http.Request, address string) (*core.Identity, bool) {
	identity, err := s.lookupNamed(r, address)
	switch {
	case err == nil:
		return identity, true
	case errors.Is(err, errNotPublic):
		notFound(w)
		return nil, false
	case !errors.Is(err, pg.ErrNotFound):
		internalError(w, r, "resolving a named inbox", err)
		return nil, false
	}

	localPart, domainName := splitNamed(address)
	var domain *core.Domain
	if domainName != "" {
		domain, err = s.allocatablePublicDomain(r, domainName)
		if errors.Is(err, pg.ErrNotFound) {
			notFound(w)
			return nil, false
		}
		if err != nil {
			internalError(w, r, "resolving a public domain", err)
			return nil, false
		}
	}

	if !s.namedRate.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, codeRateLimited,
			"Too many new addresses from your network, try again later")
		return nil, false
	}

	var result *alloc.Result
	if domain != nil {
		err = s.db.InTx(r.Context(), func(q pg.Querier) error {
			var provisionErr error
			result, provisionErr = s.allocator.ProvisionNamed(r.Context(), q, domain, localPart)
			return provisionErr
		})
	} else {
		result, err = s.allocator.ResolveOrCreateNamed(r.Context(), s.db, localPart)
	}
	switch {
	case errors.Is(err, core.ErrLocalPartSyntax),
		errors.Is(err, core.ErrLocalPartReserved),
		errors.Is(err, core.ErrLocalPartBlocked):
		// A reserved or blocked name has no inbox and never will. Saying
		// which would recite the denylist.
		notFound(w)
		return nil, false
	case errors.Is(err, alloc.ErrNoDomains):
		writeError(w, http.StatusServiceUnavailable, codeUnavailable,
			"No public domain is currently handing out addresses")
		return nil, false
	case err != nil:
		internalError(w, r, "opening a named inbox", err)
		return nil, false
	}
	return result.Identity, true
}

// allocatablePublicDomain finds a public-pool domain by name, but only one that
// is currently handing out addresses.
func (s *Server) allocatablePublicDomain(r *http.Request, name string) (*core.Domain, error) {
	domains, err := pg.AllocatableDomains(r.Context(), s.db, core.PoolPublic)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		if domains[i].Name == name {
			return &domains[i], nil
		}
	}
	return nil, pg.ErrNotFound
}
