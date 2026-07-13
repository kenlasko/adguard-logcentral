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

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/auth"
	"github.com/kenlasko/adguard-logcentral/internal/config"
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

// TestLogsRowsCarryMobileCardStructure asserts each data cell carries the class
// the mobile grid card pairs cells by, and that the row exposes the domain and
// blocked state the long-press block flow reads. The mobile view drops the
// per-cell column headers, so no data-label should remain on a log row.
func TestLogsRowsCarryMobileCardStructure(t *testing.T) {
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
	for _, cls := range []string{`class="time"`, `class="instance"`, `class="client"`, `class="domain"`, `class="qtype"`, `class="elapsed"`} {
		if !strings.Contains(body, cls) {
			t.Errorf("log rows missing cell %s for mobile card layout", cls)
		}
	}
	// The long-press flow reads these off the row.
	if !strings.Contains(body, `data-domain="a.com"`) {
		t.Error("log row should carry data-domain for the long-press block flow")
	}
	if !strings.Contains(body, `data-blocked="false"`) {
		t.Error("an allowed row should carry data-blocked=\"false\"")
	}
	// The mobile card drops the repeated per-cell headers.
	if strings.Contains(body, "data-label=") {
		t.Error("log rows should not carry data-label headers after the mobile redesign")
	}
}

// TestLogsRowMarksBlockedState verifies a blocked entry advertises
// data-blocked="true" so a long-press offers Unblock rather than Block.
func TestLogsRowMarksBlockedState(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "ads.example.com", true)})
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)
	h, cookie := authed(t, codec, s.handleLogsPartial)

	req := httptest.NewRequest("GET", "/partials/logs", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), `data-blocked="true"`) {
		t.Error("a blocked row should carry data-blocked=\"true\"")
	}
}

// TestLogsPageHasBlockModal ensures the confirmation modal the long-press flow
// opens is rendered on the full logs page.
func TestLogsPageHasBlockModal(t *testing.T) {
	c1, close1 := clientFor(t, "dns1", []adguardtest.Entry{fakeEntry("2026-07-10T00:00:04Z", "1.1.1.1", "a.com", false)})
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)
	h, cookie := authed(t, codec, s.handleLogsPage)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`id="block-modal"`, `id="block-modal-confirm"`, `id="block-modal-domain"`} {
		if !strings.Contains(body, want) {
			t.Errorf("logs page missing modal element %q", want)
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

	// Wildcard prefix search: matches ads.example.com, excludes safe.org.
	req := httptest.NewRequest("GET", "/partials/logs?search=ads*", nil)
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
			seen[d] += strings.Count(body, d+"</button>")
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
