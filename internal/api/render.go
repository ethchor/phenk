package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/ethchor/phenk/internal/api/apigen"
)

// Error codes returned to clients. They are stable strings a caller can branch
// on, unlike the human-readable message beside them.
const (
	codeBadRequest   = "bad_request"
	codeNotFound     = "not_found"
	codeRateLimited  = "rate_limited"
	codeUnavailable  = "unavailable"
	codeInternal     = "internal"
	codeNotPermitted = "not_permitted"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Nothing this API returns should ever be cached by an intermediary: it is
	// all either private mail or a cursor that moves.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Debug("writing response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apigen.Error{
		Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: code, Message: message},
	})
}

// notFound is the single response for "no such thing" and "not yours".
//
// Keeping them indistinguishable is the point: a caller that could tell an
// address which never existed from one belonging to someone else could
// enumerate who uses the service, which is the same leak the SMTP surface
// refuses.
func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, codeNotFound, "Not found")
}

func badRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, codeBadRequest, message)
}

func internalError(w http.ResponseWriter, r *http.Request, what string, err error) {
	slog.Error("request failed", "path", r.URL.Path, "operation", what, "error", err)
	// The client is told nothing about the failure: an error string can carry
	// a query, a hostname, or a key id, and none of that is the caller's.
	writeError(w, http.StatusInternalServerError, codeInternal, "Something went wrong")
}

// decodeJSON reads a request body, refusing unknown fields so a typo in a
// client is an error rather than a silently ignored setting.
//
// An absent body is not an error. Several endpoints take an optional body, and
// a caller who wants every default should be able to send nothing at all.
// Handlers that need a field check for it themselves.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	if r.Body == nil {
		return true
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		badRequest(w, "The request body could not be read: "+err.Error())
		return false
	}
	return true
}
