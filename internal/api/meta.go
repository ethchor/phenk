package api

import (
	"net/http"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// codeFeatureDisabled marks a request for a surface the operator turned off.
const codeFeatureDisabled = "feature_disabled"

// GetMeta implements apigen.ServerInterface.
//
// Everything here is public and changes only when an operator reconfigures the
// server, so it is safe to cache briefly. The app reads it once on load to
// decide whether to offer private addresses at all.
func (s *Server) GetMeta(w http.ResponseWriter, r *http.Request) {
	domains, err := s.offeredDomains(r)
	if err != nil {
		internalError(w, r, "listing domains", err)
		return
	}

	meta := apigen.Meta{
		Domains:              domains,
		PublicRetentionHours: int(s.cfg.PublicRetention.Hours()),
		MaxWaitSeconds:       int(s.cfg.MaxWaitTimeout.Seconds()),
		Version:              s.cfg.Version,
	}
	meta.Features.Disposable = s.cfg.Disposable

	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, meta)
}

// offeredDomains lists the domains currently handing out addresses.
//
// Random-pool domains are left out while private addresses are turned off. They
// still receive mail for any private address created before the feature was
// switched off, but they hand out nothing new, and listing them would suggest
// otherwise.
func (s *Server) offeredDomains(r *http.Request) ([]apigen.Domain, error) {
	pools := []core.Pool{core.PoolPublic}
	if s.cfg.Disposable {
		pools = []core.Pool{core.PoolRandom, core.PoolPublic}
	}

	out := []apigen.Domain{}
	for _, pool := range pools {
		domains, err := pg.AllocatableDomains(r.Context(), s.db, pool)
		if err != nil {
			return nil, err
		}
		for _, domain := range domains {
			out = append(out, apigen.Domain{
				Name: domain.Name,
				Pool: apigen.DomainPool(domain.Pool),
			})
		}
	}
	return out, nil
}
