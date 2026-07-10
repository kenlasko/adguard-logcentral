package auth

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

	"github.com/kenlasko/adguard-log-aggregator/internal/auth/oidctest"
	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildApp wires an Authenticator against the fake issuer and mounts it (plus a
// trivial home page) on an httptest server. It returns the server and issuer.
func buildApp(t *testing.T, iss *oidctest.Issuer) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("home"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	codec, err := NewCodec(strings.Repeat("s", 32), false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		OIDCIssuerURL:    iss.URL(),
		OIDCClientID:     iss.ClientID,
		OIDCClientSecret: "client-secret",
		OIDCRedirectURL:  srv.URL + "/auth/callback",
		SessionDuration:  time.Hour,
	}
	auth, err := NewAuthenticator(context.Background(), cfg, codec, discardLogger())
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	auth.Routes(mux)
	return srv
}

func jarClient(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func TestOIDCFullFlowHappyPath(t *testing.T) {
	iss := oidctest.New("client-id", "user-42", "Grace Hopper", "grace@example.com")
	defer iss.Close()
	app := buildApp(t, iss)
	client := jarClient(t)

	resp, err := client.Get(app.URL + "/auth/login")
	if err != nil {
		t.Fatalf("login flow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("final status = %d, want 200 (home)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "home" {
		t.Errorf("expected to land on home, got %q", body)
	}

	// A session cookie must now be set for the app origin.
	u, _ := resp.Request.URL.Parse("/")
	var found bool
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == sessionCookieName {
			found = true
		}
	}
	if !found {
		t.Error("no session cookie set after successful login")
	}
}

func TestOIDCStateMismatchRejected(t *testing.T) {
	iss := oidctest.New("client-id", "u", "n", "e")
	defer iss.Close()
	app := buildApp(t, iss)
	client := jarClient(t)

	// Kick off login to obtain a valid flow cookie, but stop before following.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Get(app.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Now call the callback with a bogus state; the flow cookie is in the jar.
	client.CheckRedirect = nil
	cb, err := client.Get(app.URL + "/auth/callback?state=WRONG&code=abc")
	if err != nil {
		t.Fatal(err)
	}
	defer cb.Body.Close()
	if cb.StatusCode != http.StatusBadRequest {
		t.Errorf("state mismatch got %d, want 400", cb.StatusCode)
	}
}

func TestOIDCBadNonceRejected(t *testing.T) {
	iss := oidctest.New("client-id", "u", "n", "e")
	iss.ForceNonce = "attacker-controlled-nonce"
	defer iss.Close()
	app := buildApp(t, iss)
	client := jarClient(t)

	resp, err := client.Get(app.URL + "/auth/login")
	if err != nil {
		t.Fatalf("flow: %v", err)
	}
	defer resp.Body.Close()
	// The callback rejects the mismatched nonce with 401.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad nonce got %d, want 401", resp.StatusCode)
	}
}

func TestSafeReturnTo(t *testing.T) {
	cases := map[string]string{
		"":                 "/",
		"/stats":           "/stats",
		"//evil.com":       "/",
		"https://evil.com": "/",
		"/logs?search=x":   "/logs?search=x",
	}
	for in, want := range cases {
		if got := safeReturnTo(in); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
