package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/auth"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

// blockSetup wires a Server to a single fake instance and returns the fake (to
// assert persisted rules) alongside an authenticated handler and session
// cookie for the block endpoint.
func blockSetup(t *testing.T) (*adguardtest.Fake, http.Handler, *http.Cookie) {
	t.Helper()
	fake := adguardtest.New("dns1", "u", "p", nil)
	srv := fake.Server()
	t.Cleanup(srv.Close)

	client := adguard.New(config.Instance{Name: "dns1", URL: srv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{client}, 50)
	h, cookie := authed(t, codec, s.handleBlock)
	return fake, h, cookie
}

func postBlock(h http.Handler, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/partials/block", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBlockAddsRuleAndOffersUnblock(t *testing.T) {
	fake, h, cookie := blockSetup(t)

	rec := postBlock(h, cookie, url.Values{
		"instance": {"dns1"},
		"domain":   {"ads.example.com"},
		"action":   {"block"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := fake.Rules(); len(got) != 1 || got[0] != "||ads.example.com^" {
		t.Fatalf("rules after block = %v", got)
	}
	if !strings.Contains(rec.Body.String(), "Unblock") {
		t.Errorf("response should offer Unblock, got %q", rec.Body.String())
	}
}

func TestUnblockAddsAllowRule(t *testing.T) {
	fake, h, cookie := blockSetup(t)

	rec := postBlock(h, cookie, url.Values{
		"instance": {"dns1"},
		"domain":   {"good.example.com"},
		"action":   {"unblock"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := fake.Rules(); len(got) != 1 || got[0] != "@@||good.example.com^" {
		t.Fatalf("rules after unblock = %v", got)
	}
	if !strings.Contains(rec.Body.String(), "Block") {
		t.Errorf("response should offer Block, got %q", rec.Body.String())
	}
}

func TestBlockTrimsTrailingDot(t *testing.T) {
	fake, h, cookie := blockSetup(t)

	rec := postBlock(h, cookie, url.Values{
		"instance": {"dns1"},
		"domain":   {"ads.example.com."},
		"action":   {"block"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := fake.Rules(); len(got) != 1 || got[0] != "||ads.example.com^" {
		t.Fatalf("rules = %v, want the trailing dot stripped", got)
	}
}

func TestBlockRejectsUnknownInstance(t *testing.T) {
	fake, h, cookie := blockSetup(t)

	rec := postBlock(h, cookie, url.Values{
		"instance": {"nope"},
		"domain":   {"ads.example.com"},
		"action":   {"block"},
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if got := fake.Rules(); len(got) != 0 {
		t.Fatalf("no rule should be set for an unknown instance, got %v", got)
	}
}

func TestBlockRejectsInvalidDomain(t *testing.T) {
	fake, h, cookie := blockSetup(t)

	rec := postBlock(h, cookie, url.Values{
		"instance": {"dns1"},
		"domain":   {"bad^domain||evil"},
		"action":   {"block"},
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if got := fake.Rules(); len(got) != 0 {
		t.Fatalf("no rule should be set for an invalid domain, got %v", got)
	}
}

func TestBlockRequiresAuth(t *testing.T) {
	fake := adguardtest.New("dns1", "u", "p", nil)
	srv := fake.Server()
	t.Cleanup(srv.Close)
	client := adguard.New(config.Instance{Name: "dns1", URL: srv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{client}, 50)

	// Wrap the handler in real auth middleware but send no session cookie.
	mw := auth.NewMiddleware(codec)
	h := mw.RequireAuth(http.HandlerFunc(s.handleBlock))

	req := httptest.NewRequest("POST", "/partials/block", strings.NewReader(url.Values{
		"instance": {"dns1"}, "domain": {"ads.example.com"}, "action": {"block"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated block = %d, want 401", rec.Code)
	}
	if got := fake.Rules(); len(got) != 0 {
		t.Fatalf("unauthenticated request must not change rules, got %v", got)
	}
}

func TestValidDomain(t *testing.T) {
	valid := []string{"example.com", "a.b.c.example.com", "_dmarc.example.com", "xn--80ak6aa92e.com", "host-1.io"}
	for _, d := range valid {
		if !validDomain(d) {
			t.Errorf("validDomain(%q) = false, want true", d)
		}
	}
	invalid := []string{"", "no space.com", "bad^.com", "a||b", "@@evil.com", "-lead.com", "trail-.com", ".leadingdot", "under_score.ok.but.this..double"}
	for _, d := range invalid {
		if validDomain(d) {
			t.Errorf("validDomain(%q) = true, want false", d)
		}
	}
}
