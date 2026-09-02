package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// routePattern returns the matched route template, or the method alone when
// nothing matched. It never returns the raw path, which would put identity and
// message ids into the logs.
func routePattern(r *http.Request) string {
	if ctx := chi.RouteContext(r.Context()); ctx != nil {
		if pattern := ctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}
