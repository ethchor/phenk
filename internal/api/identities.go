package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// CreateIdentity implements apigen.ServerInterface.
func (s *Server) CreateIdentity(w http.ResponseWriter, r *http.Request) {
	var request apigen.CreateIdentityRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	// A named address is a public inbox however it is asked for, so it goes
	// through the same code path and the same rate limit as POST /v1/named
	// rather than a second, laxer one.
	if request.Kind != nil && *request.Kind == apigen.CreateIdentityRequestKindNamed {
		if request.LocalPart == nil || *request.LocalPart == "" {
			badRequest(w, "A named address needs a local_part")
			return
		}
		s.openNamed(w, r, *request.LocalPart, http.StatusCreated)
		return
	}

	ttl := s.cfg.DefaultTTL
	if request.TtlSeconds != nil {
		ttl = time.Duration(*request.TtlSeconds) * time.Second
	}

	// The session cookie is set before anything else is written, and is what
	// ownership means in v0.
	session := s.ensureSession(w, r)

	result, err := s.allocator.AllocateRandom(r.Context(), s.db, session, ttl)
	switch {
	case errors.Is(err, alloc.ErrNoDomains):
		writeError(w, http.StatusServiceUnavailable, codeUnavailable,
			"No domain is currently handing out addresses")
		return
	case err != nil:
		internalError(w, r, "allocating an address", err)
		return
	}

	writeJSON(w, http.StatusCreated, identityResponse(result.Identity, result.Domain.Name))
}

// GetIdentity implements apigen.ServerInterface.
func (s *Server) GetIdentity(w http.ResponseWriter, r *http.Request, id apigen.IdentityId) {
	identity, domain, ok := s.ownedIdentity(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, identityResponse(identity, domain.Name))
}

// DestroyIdentity implements apigen.ServerInterface.
//
// The identity is expired rather than deleted. Purge destroys its data key on
// the next run, and the row stays behind forever as a tombstone so the address
// is never handed to anyone else.
func (s *Server) DestroyIdentity(w http.ResponseWriter, r *http.Request, id apigen.IdentityId) {
	identity, _, ok := s.ownedIdentity(w, r, id)
	if !ok {
		return
	}

	moved, err := pg.SetIdentityState(r.Context(), s.db, identity.ID, core.IdentityExpired,
		core.IdentityActive, core.IdentityExpiring)
	if err != nil {
		internalError(w, r, "expiring an identity", err)
		return
	}
	if moved {
		if _, err := pg.AppendEvent(r.Context(), s.db, &identity.ID, core.EventIdentityExpired,
			map[string]any{"reason": "destroyed by owner"}); err != nil {
			internalError(w, r, "recording the expiry", err)
			return
		}
	}
	// Repeating the call is safe and reports the same thing, because the
	// caller's intent — that this address is finished — already holds.
	w.WriteHeader(http.StatusNoContent)
}

// ownedIdentity resolves an identity for the /v1/identities routes.
//
// It refuses anything that is not a random identity owned by this session. A
// named identity has no owner and so can never be reached here, which is
// checked explicitly rather than left to follow from the empty owner column:
// invariant 9 is worth stating in the code that enforces it.
func (s *Server) ownedIdentity(w http.ResponseWriter, r *http.Request, id apigen.IdentityId) (*core.Identity, *core.Domain, bool) {
	identityID, err := core.ParseUUID(id.String())
	if err != nil {
		notFound(w)
		return nil, nil, false
	}

	identity, err := pg.IdentityByID(r.Context(), s.db, identityID)
	if errors.Is(err, pg.ErrNotFound) {
		notFound(w)
		return nil, nil, false
	}
	if err != nil {
		internalError(w, r, "loading an identity", err)
		return nil, nil, false
	}

	if identity.Kind != core.KindRandom {
		// A named inbox is public and lives behind /v1/named. Serving it here
		// would imply an ownership that does not exist.
		notFound(w)
		return nil, nil, false
	}
	session := s.session(r)
	if session == "" || identity.OwnerSession != session {
		notFound(w)
		return nil, nil, false
	}

	domain, err := pg.DomainByID(r.Context(), s.db, identity.DomainID)
	if err != nil {
		internalError(w, r, "loading a domain", err)
		return nil, nil, false
	}
	return identity, domain, true
}

// identityResponse renders an identity for the API.
func identityResponse(identity *core.Identity, domainName string) apigen.Identity {
	out := apigen.Identity{
		Id:        mustAPIUUID(identity.ID),
		Address:   identity.LocalPart + "@" + domainName,
		LocalPart: identity.LocalPart,
		Domain:    domainName,
		Kind:      apigen.IdentityKind(identity.Kind),
		State:     apigen.IdentityState(identity.State),
		Cursor:    identity.DeliverySeq,
		// Named inboxes are readable by anyone who guesses the name. Every
		// surface that shows one is required to say so, and this is the flag
		// that tells it to.
		Public:    identity.Kind == core.KindNamed,
		CreatedAt: identity.CreatedAt,
		Quota: apigen.Quota{
			Messages:     identity.QuotaMessages,
			Bytes:        identity.QuotaBytes,
			UsedMessages: identity.UsedMessages,
			UsedBytes:    identity.UsedBytes,
		},
	}
	if identity.ExpiresAt != nil {
		out.ExpiresAt = identity.ExpiresAt
	}
	if identity.RetentionHours != nil {
		out.RetentionHours = identity.RetentionHours
	}
	return out
}
