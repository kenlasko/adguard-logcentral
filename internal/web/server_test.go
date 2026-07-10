package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/auth"
	"github.com/kenlasko/adguard-logcentral/internal/auth/oidctest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

// TestEndToEndThroughFullStack drives the complete server -- routing, auth
// middleware, OIDC login, and aggregation -- against fake instances and a fake
// issuer, mirroring the DESIGN.md verification checklist.
func TestEndToEndThroughFullStack(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "d1.example", false)})
	defer close1()
	c2, close2 := clientFor(t, "dns2", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:03Z", "2.2.2.2", "d2.example", false)})
	defer close2()

	iss := oidctest.New("client-id", "user-1", "Ada", "ada@example.com")
	defer iss.Close()

	// Indirection lets us build the server after the test URL is known.
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	defer ts.Close()

	codec, err := auth.NewCodec(strings.Repeat("k", 32), false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Instances:        []config.Instance{{Name: "dns1"}, {Name: "dns2"}},
		AdGuardTimeout:   2 * time.Second,
		PageSize:         50,
		OIDCIssuerURL:    iss.URL(),
		OIDCClientID:     "client-id",
		OIDCClientSecret: "secret",
		OIDCRedirectURL:  ts.URL + "/auth/callback",
		SessionDuration:  time.Hour,
	}
	authn, err := auth.NewAuthenticator(context.Background(), cfg, codec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	srv, err := NewServer(cfg, []*adguard.Client{c1, c2}, authn, auth.NewMiddleware(codec), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler = srv.Handler()

	// /healthz works without any session.
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz without auth = %d, want 200", resp.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Hitting / unauthenticated should walk the full OIDC login and land back
	// on the logs page with merged rows from both instances.
	home, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("home flow: %v", err)
	}
	defer home.Body.Close()
	body, _ := io.ReadAll(home.Body)
	if home.StatusCode != http.StatusOK {
		t.Fatalf("home after login = %d, want 200", home.StatusCode)
	}
	for _, want := range []string{"d1.example", "d2.example", "dns1", "dns2"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("logs page missing %q after login", want)
		}
	}

	// Stats page reachable with the established session.
	stats, err := client.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer stats.Body.Close()
	sb, _ := io.ReadAll(stats.Body)
	if stats.StatusCode != http.StatusOK || !strings.Contains(string(sb), "DNS queries") {
		t.Errorf("stats page not rendered: status %d", stats.StatusCode)
	}

	// Health partial reachable and shows both instances up.
	health, err := client.Get(ts.URL + "/partials/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	hb, _ := io.ReadAll(health.Body)
	if !strings.Contains(string(hb), "dns1") || !strings.Contains(string(hb), "dns2") {
		t.Errorf("health partial missing instances: %s", hb)
	}
}
