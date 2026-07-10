package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
	"github.com/kenlasko/adguard-log-aggregator/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-log-aggregator/internal/auth"
	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

// testServer builds a Server wired to the given clients, with real templates
// and a discard logger, for handler-level tests. Auth is exercised via a real
// middleware and forged session cookie rather than a live OIDC provider.
func testServer(t *testing.T, instances []string, clients []*adguard.Client, pageSize int) (*Server, *auth.Codec) {
	t.Helper()
	tmpls, err := newTemplates(instances)
	if err != nil {
		t.Fatalf("newTemplates: %v", err)
	}
	codec, err := auth.NewCodec(strings.Repeat("k", 32), false)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg:       config.Config{PageSize: pageSize, AdGuardTimeout: 2 * time.Second},
		clients:   clients,
		instances: instances,
		templates: tmpls,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return s, codec
}

// authed wraps a handler in real auth middleware and returns a request already
// carrying a valid forged session cookie.
func authed(t *testing.T, codec *auth.Codec, h http.HandlerFunc) (http.Handler, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := codec.WriteSession(rec, auth.Session{Sub: "tester", Email: "t@example.com", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	mw := auth.NewMiddleware(codec)
	return mw.RequireAuth(h), rec.Result().Cookies()[0]
}

func fakeEntry(raw, client, domain string, blocked bool) adguardtest.Entry {
	e := adguardtest.Entry{Time: raw, Client: client, Domain: domain, QType: "A", Reason: "NotFilteredNotFound", Status: "processed", Elapsed: "5"}
	if blocked {
		e.Blocked = true
		e.Reason = "FilteredBlackList"
		e.Status = "blocked"
		e.RuleText = "||" + domain + "^"
	}
	return e
}

func clientFor(t *testing.T, name string, entries []adguardtest.Entry) (*adguard.Client, func()) {
	t.Helper()
	srv := adguardtest.New(name, "u", "p", entries).Server()
	c := adguard.New(config.Instance{Name: name, URL: srv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})
	return c, srv.Close
}

func TestLogsPageMergesInstancesWithBadges(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "a.com", false)})
	defer close1()
	c2, close2 := clientFor(t, "dns2", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:03Z", "2.2.2.2", "b.com", false)})
	defer close2()

	s, codec := testServer(t, []string{"dns1", "dns2"}, []*adguard.Client{c1, c2}, 50)
	h, cookie := authed(t, codec, s.handleLogsPage)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(body, "<html") {
		t.Error("full page should contain <html>")
	}
	for _, want := range []string{"dns1", "dns2", "a.com", "b.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestLogsPartialIsFragmentNotFullPage(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "a.com", false)})
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)
	h, cookie := authed(t, codec, s.handleLogsPartial)

	req := httptest.NewRequest("GET", "/partials/logs", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Error("fragment should not contain <html>")
	}
	if !strings.Contains(body, "a.com") {
		t.Error("fragment missing row")
	}
}

func TestLogsFilterForwardedToInstances(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{
		fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "ads.example.com", true),
		fakeEntry("2026-07-10T00:00:03Z", "1.1.1.1", "safe.org", false),
	})
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)
	h, cookie := authed(t, codec, s.handleLogsPartial)

	req := httptest.NewRequest("GET", "/partials/logs?search=ads", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "ads.example.com") {
		t.Error("search should include matching domain")
	}
	if strings.Contains(body, "safe.org") {
		t.Error("search should exclude non-matching domain")
	}
}

func TestLogsDownedInstanceShowsWarningWithSurvivingRows(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "a.com", false)})
	defer close1()
	// dns2 always errors.
	downSrv := adguardtest.New("dns2", "u", "p", nil).WithFailures(adguardtest.Failures{QueryLog500: true}).Server()
	defer downSrv.Close()
	c2 := adguard.New(config.Instance{Name: "dns2", URL: downSrv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})

	s, codec := testServer(t, []string{"dns1", "dns2"}, []*adguard.Client{c1, c2}, 50)
	h, cookie := authed(t, codec, s.handleLogsPartial)

	req := httptest.NewRequest("GET", "/partials/logs", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Partial results") || !strings.Contains(body, "dns2") {
		t.Error("expected partial-results warning naming dns2")
	}
	if !strings.Contains(body, "a.com") {
		t.Error("surviving dns1 row should still appear")
	}
}

var cursorRe = regexp.MustCompile(`cursor=([A-Za-z0-9_-]+)`)

func TestLogsCursorRoundTripNoBoundaryLoss(t *testing.T) {
	// Interleaved across two instances; pageSize 2 forces multiple pages.
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{
		fakeEntry("2026-07-10T00:00:06Z", "1.1.1.1", "d1a.com", false),
		fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "d1b.com", false),
		fakeEntry("2026-07-10T00:00:02Z", "1.1.1.1", "d1c.com", false),
	})
	defer close1()
	c2, close2 := clientFor(t, "dns2", []adguardtest.Entry{
		fakeEntry("2026-07-10T00:00:05Z", "2.2.2.2", "d2a.com", false),
		fakeEntry("2026-07-10T00:00:03Z", "2.2.2.2", "d2b.com", false),
		fakeEntry("2026-07-10T00:00:01Z", "2.2.2.2", "d2c.com", false),
	})
	defer close2()

	s, codec := testServer(t, []string{"dns1", "dns2"}, []*adguard.Client{c1, c2}, 2)
	h, cookie := authed(t, codec, s.handleLogsPartial)

	seen := map[string]int{}
	url := "/partials/logs"
	for page := 0; page < 20; page++ {
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("HX-Request", "true")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		for _, d := range []string{"d1a.com", "d1b.com", "d1c.com", "d2a.com", "d2b.com", "d2c.com"} {
			seen[d] += strings.Count(body, d+"</td>")
		}
		m := cursorRe.FindStringSubmatch(body)
		if m == nil {
			break // no more pages
		}
		url = "/partials/logs?cursor=" + m[1]
	}

	for _, d := range []string{"d1a.com", "d1b.com", "d1c.com", "d2a.com", "d2b.com", "d2c.com"} {
		if seen[d] != 1 {
			t.Errorf("domain %s appeared %d times across pages, want exactly 1", d, seen[d])
		}
	}
}
