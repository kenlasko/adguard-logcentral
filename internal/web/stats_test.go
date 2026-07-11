package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

func statsClient(t *testing.T, name string, payload adguardtest.StatsPayload) (*adguard.Client, func()) {
	t.Helper()
	srv := adguardtest.New(name, "u", "p", nil).WithStats(payload).Server()
	c := adguard.New(config.Instance{Name: name, URL: srv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})
	return c, srv.Close
}

func TestStatsPageShowsMergedTotalsAndTopN(t *testing.T) {
	c1, close1 := statsClient(t, "dns1", adguardtest.StatsPayload{
		NumDNSQueries:       1000,
		NumBlockedFiltering: 250,
		AvgProcessingTime:   0.010,
		TopBlockedDomains:   []map[string]int64{{"ads.example.com": 200}},
		TopClients:          []map[string]int64{{"1.1.1.1": 1000}},
		TopQueriedDomains:   []map[string]int64{{"good.com": 500}},
	})
	defer close1()
	c2, close2 := statsClient(t, "dns2", adguardtest.StatsPayload{
		NumDNSQueries:       1000,
		NumBlockedFiltering: 250,
		AvgProcessingTime:   0.020,
		TopBlockedDomains:   []map[string]int64{{"ads.example.com": 100}},
		TopClients:          []map[string]int64{{"2.2.2.2": 1000}},
		TopQueriedDomains:   []map[string]int64{{"good.com": 300}},
	})
	defer close2()

	s, codec := testServer(t, []string{"dns1", "dns2"}, []*adguard.Client{c1, c2}, 50)
	h, cookie := authed(t, codec, s.handleStatsPage)

	req := httptest.NewRequest("GET", "/stats", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	// Merged queries: 2,000; merged blocked: 500; blocked share 25.0%.
	if !strings.Contains(body, "2,000") {
		t.Errorf("expected merged query total 2,000: %s", body)
	}
	if !strings.Contains(body, "25.0%") {
		t.Errorf("expected 25.0%% blocked share")
	}
	// ads.example.com merged 200+100 = 300 blocked.
	if !strings.Contains(body, "ads.example.com") || !strings.Contains(body, "300") {
		t.Error("expected merged top blocked domain with combined count")
	}
	// Per-instance breakdown table present, each instance on its own row.
	if !strings.Contains(body, "Statistics by instance") {
		t.Error("expected per-instance breakdown table heading")
	}
	if !strings.Contains(body, "dns1") || !strings.Contains(body, "dns2") {
		t.Error("expected per-instance breakdown badges")
	}
	// Each instance's own blocked share: 250/1000 = 25.0% appears per row.
	if strings.Count(body, "25.0%") < 3 {
		t.Errorf("expected merged 25.0%% plus a 25.0%% row for each instance")
	}
}

func TestStatsPartialIsFragment(t *testing.T) {
	c1, close1 := statsClient(t, "dns1", adguardtest.StatsPayload{NumDNSQueries: 10, NumBlockedFiltering: 1})
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)
	h, cookie := authed(t, codec, s.handleStatsPartial)

	req := httptest.NewRequest("GET", "/partials/stats", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Error("stats fragment should not contain <html>")
	}
	if !strings.Contains(body, "DNS queries") {
		t.Error("stats fragment missing tiles")
	}
}

// TestStatsTablesCarryResponsiveLabels asserts the per-instance and top-N
// tables tag their cells with data-label so the mobile card layout can show
// the column name beside each value.
func TestStatsTablesCarryResponsiveLabels(t *testing.T) {
	c1, close1 := statsClient(t, "dns1", adguardtest.StatsPayload{
		NumDNSQueries:       10,
		NumBlockedFiltering: 1,
		TopBlockedDomains:   []map[string]int64{{"ads.example.com": 5}},
	})
	defer close1()
	s, codec := testServer(t, []string{"dns1"}, []*adguard.Client{c1}, 50)
	h, cookie := authed(t, codec, s.handleStatsPartial)

	req := httptest.NewRequest("GET", "/partials/stats", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, label := range []string{"Queries", "Blocked %", "Avg processing", "By instance"} {
		if !strings.Contains(body, `data-label="`+label+`"`) {
			t.Errorf("stats tables missing data-label %q for responsive layout", label)
		}
	}
}

func TestStatsPartialFailureBanner(t *testing.T) {
	c1, close1 := statsClient(t, "dns1", adguardtest.StatsPayload{NumDNSQueries: 10})
	defer close1()
	down := adguardtest.New("dns2", "u", "p", nil).WithFailures(adguardtest.Failures{Stats500: true}).Server()
	defer down.Close()
	c2 := adguard.New(config.Instance{Name: "dns2", URL: down.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})

	s, codec := testServer(t, []string{"dns1", "dns2"}, []*adguard.Client{c1, c2}, 50)
	h, cookie := authed(t, codec, s.handleStatsPartial)

	req := httptest.NewRequest("GET", "/partials/stats", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "Partial results") {
		t.Error("expected partial-results banner when an instance's stats fail")
	}
}
