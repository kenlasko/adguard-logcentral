package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sess, ok := SessionFrom(r.Context()); ok {
			_, _ = w.Write([]byte("hello " + sess.Sub))
			return
		}
		_, _ = w.Write([]byte("no-session"))
	})
}

func TestMiddlewareAllowlist(t *testing.T) {
	mw := NewMiddleware(testCodec(t))
	h := mw.RequireAuth(okHandler())

	for _, path := range []string{"/healthz", "/auth/login", "/static/app.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("allowlisted %s returned %d, want 200", path, rec.Code)
		}
	}
}

func TestMiddlewareBrowserRedirect(t *testing.T) {
	mw := NewMiddleware(testCodec(t))
	h := mw.RequireAuth(okHandler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("unauthenticated browser request got %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Errorf("redirect location = %q, want /auth/login", loc)
	}
}

func TestMiddlewareHTMXRedirect(t *testing.T) {
	mw := NewMiddleware(testCodec(t))
	h := mw.RequireAuth(okHandler())

	req := httptest.NewRequest("GET", "/partials/logs", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("htmx unauthenticated got %d, want 401", rec.Code)
	}
	if hx := rec.Header().Get("HX-Redirect"); hx != "/auth/login" {
		t.Errorf("HX-Redirect = %q, want /auth/login", hx)
	}
}

func TestMiddlewareValidSession(t *testing.T) {
	codec := testCodec(t)
	mw := NewMiddleware(codec)
	h := mw.RequireAuth(okHandler())

	req := httptest.NewRequest("GET", "/", nil)
	rec0 := httptest.NewRecorder()
	_ = codec.WriteSession(rec0, Session{Sub: "abc", Expiry: time.Now().Add(time.Hour)})
	req.AddCookie(rec0.Result().Cookies()[0])

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid session got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "hello abc") {
		t.Errorf("session not placed in context: %q", rec.Body.String())
	}
}

func TestMiddlewareExpiredSessionRejected(t *testing.T) {
	codec := testCodec(t)
	mw := NewMiddleware(codec)
	h := mw.RequireAuth(okHandler())

	req := httptest.NewRequest("GET", "/", nil)
	// Seal a session that is already expired.
	token, _ := codec.SealSession(Session{Sub: "abc", Expiry: time.Now().Add(-time.Hour)})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("expired session should redirect, got %d", rec.Code)
	}
}
