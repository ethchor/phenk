package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"
)

// sessionBytes is the entropy behind an owner session. This value is the only
// thing standing between a stranger and someone's inbox in v0, so it is sized
// like a credential rather than an identifier.
const sessionBytes = 32

// sessionMaxAge outlives any address the session can create, so a browser left
// open overnight still owns what it made.
const sessionMaxAge = 30 * 24 * time.Hour

// session returns the caller's owner session, or the empty string if they have
// none yet.
func (s *Server) session(r *http.Request) string {
	cookie, err := r.Cookie(s.cfg.CookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return cookie.Value
}

// ensureSession returns the caller's session, minting and setting one if they
// have none. It must be called before anything is written to the response.
func (s *Server) ensureSession(w http.ResponseWriter, r *http.Request) string {
	if existing := s.session(r); existing != "" {
		return existing
	}

	buf := make([]byte, sessionBytes)
	if _, err := rand.Read(buf); err != nil {
		panic("api: random source unavailable: " + err.Error())
	}
	value := base64.RawURLEncoding.EncodeToString(buf)

	http.SetCookie(w, &http.Cookie{
		Name:  s.cfg.CookieName,
		Value: value,
		Path:  "/",
		// Script has no reason to read this, and keeping it out of the DOM
		// removes a whole class of ways to steal an inbox.
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		// Lax rather than Strict: a user following a link from the marketing
		// site to their inbox should arrive still owning it.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})
	return value
}
