package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
)

// TestSecurityHeadersMiddleware verifies the defense-in-depth response headers
// are set on every response passing through securityHeaders.
func TestSecurityHeadersMiddleware(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for h, v := range want {
		if got := rec.Header().Get(h); got != v {
			t.Errorf("header %s = %q, want %q", h, got, v)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header missing")
	}
	for _, directive := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP %q missing directive %q", csp, directive)
		}
	}
}

// TestAuthenticatedResponsesAreNoStore verifies rendered pages and fragments
// carry Cache-Control: no-store so query-log data is not cached by browsers or
// intermediaries.
func TestAuthenticatedResponsesAreNoStore(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "a.com", false)})
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"page", s.handleLogsPage, "/"},
		{"fragment", s.handleLogsPartial, "/partials/logs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, cookie := authed(t, codec, tc.handler)
			req := httptest.NewRequest("GET", tc.path, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s Cache-Control = %q, want no-store", tc.name, got)
			}
		})
	}
}
