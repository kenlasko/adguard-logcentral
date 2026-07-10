package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
	"github.com/kenlasko/adguard-log-aggregator/internal/adguard/adguardtest"
)

func TestLogsEmptyState(t *testing.T) {
	// Instance reachable but with no entries at all.
	c1, close1 := clientFor(t, "dns1", nil)
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)
	h, cookie := authed(t, codec, s.handleLogsPartial)

	req := httptest.NewRequest("GET", "/partials/logs", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "No query log entries") {
		t.Errorf("expected empty-state row, got: %s", rec.Body.String())
	}
}

func TestLoadMoreCarriesIndicator(t *testing.T) {
	entries := []adguardtest.Entry{
		fakeEntry("2026-07-10T00:00:03Z", "1.1.1.1", "a.com", false),
		fakeEntry("2026-07-10T00:00:02Z", "1.1.1.1", "b.com", false),
		fakeEntry("2026-07-10T00:00:01Z", "1.1.1.1", "c.com", false),
	}
	c1, close1 := clientFor(t, "dns1", entries)
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 2)
	h, cookie := authed(t, codec, s.handleLogsPartial)

	req := httptest.NewRequest("GET", "/partials/logs", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `id="load-more"`) {
		t.Error("expected load-more sentinel when more pages remain")
	}
	if !strings.Contains(body, "htmx-indicator") {
		t.Error("expected htmx loading indicator in load-more button")
	}
}

func TestEmbeddedStaticAssetsPresent(t *testing.T) {
	htmx, err := fs.ReadFile(staticFS, "static/htmx.min.js")
	if err != nil {
		t.Fatalf("htmx asset missing: %v", err)
	}
	if len(htmx) < 10000 || !strings.Contains(string(htmx[:200]), "htmx") {
		t.Errorf("htmx asset looks wrong (len %d)", len(htmx))
	}
	css, err := fs.ReadFile(staticFS, "static/app.css")
	if err != nil {
		t.Fatalf("css asset missing: %v", err)
	}
	if !strings.Contains(string(css), "prefers-color-scheme: dark") {
		t.Error("css should include a dark-mode block")
	}
}

func TestStaticServedThroughHandler(t *testing.T) {
	// The static handler is allowlisted, so it needs no session. Build a minimal
	// mux mirroring the server's static wiring.
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "--accent") {
		t.Errorf("static css not served: status %d", rec.Code)
	}
}
