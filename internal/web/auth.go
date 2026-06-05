package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"net/http"
	"strings"
)

// auth guards the API with a shared bearer token. The dashboard is read-only,
// but once it can be reached beyond localhost (e.g. via --tunnel) the token
// keeps strangers out. The token is presented as `Authorization: Bearer <t>`,
// the `csdd_token` cookie, or a `?token=` query param (the last so the SSE
// EventSource — which can't set headers — and the /token/<t> magic link work).
type auth struct {
	enabled bool
	token   string
}

const tokenCookie = "csdd_token"

func newAuth(enabled bool, password string) *auth {
	a := &auth{enabled: enabled}
	if !enabled {
		return a
	}
	if password != "" {
		a.token = password
	} else {
		a.token = randomToken()
	}
	return a
}

// ok reports whether the request carries the valid token (constant-time compare).
func (a *auth) ok(r *http.Request) bool {
	if !a.enabled {
		return true
	}
	if t := bearer(r.Header.Get("Authorization")); t != "" && a.match(t) {
		return true
	}
	if c, err := r.Cookie(tokenCookie); err == nil && a.match(c.Value) {
		return true
	}
	if a.match(r.URL.Query().Get("token")) {
		return true
	}
	return false
}

func (a *auth) match(got string) bool {
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) == 1
}

// protect wraps an API handler, returning 401 when the token is missing/invalid.
func (a *auth) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.ok(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next(w, r)
	}
}

func bearer(h string) string {
	const p = "Bearer "
	if strings.HasPrefix(h, p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// randomToken returns a URL-safe, lowercase, 16-char (80-bit) token.
func randomToken() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing means the system CSPRNG is broken — never serve a
		// predictable token; abort loudly instead.
		panic("csdd web: crypto/rand unavailable: " + err.Error())
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}
