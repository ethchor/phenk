package api

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// OpenNamedInbox implements apigen.ServerInterface.
func (s *Server) OpenNamedInbox(w http.ResponseWriter, r *http.Request) {
	var request apigen.OpenNamedRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	s.openNamed(w, r, request.LocalPart, http.StatusOK)
}

// openNamed resolves or creates a public inbox. It is shared with
// POST /v1/identities so that asking for a named address by either route costs
// the same, is validated the same, and is limited the same.
func (s *Server) openNamed(w http.ResponseWriter, r *http.Request, localPart string, createdStatus int) {
	localPart = core.NormalizeLocalPart(localPart)
	if err := core.ValidateNamedLocalPart(localPart); err != nil {
		badRequest(w, namedValidationMessage(err))
		return
	}

	// Creating a public inbox is the abusable operation on this surface, and
	// it is limited exactly as the SMTP path limits the same operation.
	if !s.namedRate.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, codeRateLimited,
			"Too many new addresses from your network, try again later")
		return
	}

	result, err := s.allocator.ResolveOrCreateNamed(r.Context(), s.db, localPart)
	switch {
	case errors.Is(err, core.ErrLocalPartSyntax),
		errors.Is(err, core.ErrLocalPartReserved),
		errors.Is(err, core.ErrLocalPartBlocked):
		badRequest(w, namedValidationMessage(err))
		return
	case errors.Is(err, alloc.ErrNoDomains):
		writeError(w, http.StatusServiceUnavailable, codeUnavailable,
			"No public domain is currently handing out addresses")
		return
	case err != nil:
		internalError(w, r, "opening a named inbox", err)
		return
	}

	status := http.StatusOK
	if result.Created && createdStatus != 0 {
		status = createdStatus
	}
	writeJSON(w, status, identityResponse(result.Identity, result.Domain.Name))
}

// ListNamedMessages implements apigen.ServerInterface.
func (s *Server) ListNamedMessages(w http.ResponseWriter, r *http.Request, address string, params apigen.ListNamedMessagesParams) {
	identity, ok := s.namedIdentity(w, r, address)
	if !ok {
		return
	}
	s.writeMessageList(w, r, identity, since(params.Since), limit(params.Limit))
}

// StreamNamedInbox implements apigen.ServerInterface.
func (s *Server) StreamNamedInbox(w http.ResponseWriter, r *http.Request, address string, params apigen.StreamNamedInboxParams) {
	identity, ok := s.namedIdentity(w, r, address)
	if !ok {
		return
	}
	s.stream(w, r, identity, since(params.Since))
}

// namedIdentity resolves a public inbox by name.
//
// It performs no ownership check, by design: a named inbox has no owner. It
// does check the kind, in the handler rather than only by route, so that a
// guessed identifier cannot reach a random inbox through the public door. That
// is invariant 9 read from the other direction, and it is the rule that keeps
// the shared-inbox feature out of the security model.
func (s *Server) namedIdentity(w http.ResponseWriter, r *http.Request, address string) (*core.Identity, bool) {
	localPart, domainName := splitNamed(address)
	if err := core.ValidateNamedLocalPart(localPart); err != nil {
		notFound(w)
		return nil, false
	}

	var (
		identity *core.Identity
		err      error
	)
	if domainName != "" {
		var domain *core.Domain
		domain, err = pg.DomainByName(r.Context(), s.db, domainName)
		if err == nil {
			if domain.Pool != core.PoolPublic {
				// A random-pool domain never hosts a named inbox, and asking
				// for one there must not become a way to probe it.
				notFound(w)
				return nil, false
			}
			identity, err = pg.IdentityByAddress(r.Context(), s.db, localPart, domain.ID)
		}
	} else {
		identity, _, err = pg.NamedIdentityByLocalPart(r.Context(), s.db, localPart)
	}

	if errors.Is(err, pg.ErrNotFound) {
		notFound(w)
		return nil, false
	}
	if err != nil {
		internalError(w, r, "resolving a named inbox", err)
		return nil, false
	}

	if identity.Kind != core.KindNamed {
		notFound(w)
		return nil, false
	}
	return identity, true
}

// splitNamed accepts either a bare local part or a full address.
func splitNamed(address string) (localPart, domainName string) {
	address = strings.TrimSpace(address)
	if strings.Contains(address, "@") {
		local, domain, err := core.SplitAddress(address)
		if err != nil {
			return "", ""
		}
		return local, domain
	}
	return core.NormalizeLocalPart(address), ""
}

// namedValidationMessage explains a rejection without reciting the denylist,
// which would turn the error into a way to enumerate it.
func namedValidationMessage(err error) string {
	switch {
	case errors.Is(err, core.ErrLocalPartBlocked), errors.Is(err, core.ErrLocalPartReserved):
		return "That name is reserved"
	default:
		return "A name must be 3 to 64 characters of lower-case letters, digits, dots, dashes or underscores, and must start with a letter or digit"
	}
}

// clientIP is the rate limiting key. chi's RealIP middleware has already
// resolved any proxy headers by the time this runs.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
