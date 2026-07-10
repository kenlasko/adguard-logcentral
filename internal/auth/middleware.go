package auth

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// ctxKey is the private type for context keys set by this package.
type ctxKey int

const sessionKey ctxKey = iota

// Middleware enforces a valid session on protected routes. Unauthenticated
// requests are redirected to login (browser) or told to redirect via htmx.
type Middleware struct {
	codec *Codec
	now   func() time.Time
}

// NewMiddleware builds auth middleware backed by the session codec.
func NewMiddleware(codec *Codec) *Middleware {
	return &Middleware{codec: codec, now: time.Now}
}

// allowlisted paths never require authentication.
func allowlisted(path string) bool {
	return path == "/healthz" ||
		strings.HasPrefix(path, "/auth/") ||
		strings.HasPrefix(path, "/static/")
}

// RequireAuth wraps next, enforcing a valid session except on allowlisted
// paths. A browser request without a valid session gets a 302 to /auth/login;
// an htmx request (HX-Request: true) instead gets 401 with an HX-Redirect
// header so htmx performs a full-page navigation to the login screen.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowlisted(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := m.codec.ReadSession(r, m.now())
		if err != nil {
			m.reject(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Middleware) reject(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/auth/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

// SessionFrom returns the authenticated session stored in the request context
// by RequireAuth, if any.
func SessionFrom(ctx context.Context) (Session, bool) {
	sess, ok := ctx.Value(sessionKey).(Session)
	return sess, ok
}
