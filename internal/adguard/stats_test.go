package adguard

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

func TestStatsTopNDecoding(t *testing.T) {
	body := `{
		"num_dns_queries": 100,
		"num_blocked_filtering": 40,
		"avg_processing_time": 0.012,
		"top_queried_domains": [{"a.com": 30}, {"b.com": 20}],
		"top_blocked_domains": [{"ads.net": 25}],
		"top_clients": [{"1.1.1.1": 60}, {"2.2.2.2": 40}]
	}`
	_, srv := newCapture(http.StatusOK, body)
	defer srv.Close()
	c := testClient(t, srv.URL)

	s, err := c.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.NumDNSQueries != 100 || s.NumBlockedFiltering != 40 {
		t.Errorf("scalars = %d/%d", s.NumDNSQueries, s.NumBlockedFiltering)
	}
	if len(s.TopQueriedDomains) != 2 || s.TopQueriedDomains[0].Name != "a.com" || s.TopQueriedDomains[0].Count != 30 {
		t.Errorf("top queried domains decoded wrong: %+v", s.TopQueriedDomains)
	}
	// Order must be preserved from the API array.
	if s.TopQueriedDomains[1].Name != "b.com" {
		t.Errorf("order not preserved: %+v", s.TopQueriedDomains)
	}
	if len(s.TopClients) != 2 || s.TopClients[0].Count != 60 {
		t.Errorf("top clients decoded wrong: %+v", s.TopClients)
	}
}

func TestStatusDecoding(t *testing.T) {
	body := `{"running":true,"version":"v0.107.50","protection_enabled":false}`
	_, srv := newCapture(http.StatusOK, body)
	defer srv.Close()
	c := testClient(t, srv.URL)

	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running || st.Version != "v0.107.50" || st.ProtectionEnabled {
		t.Errorf("status decoded wrong: %+v", st)
	}
}

// TestAgainstFake exercises the real client against the shared fake fixture,
// confirming pagination and auth semantics line up end-to-end.
func TestAgainstFake(t *testing.T) {
	end := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	entries := adguardtest.Generate(10, end, time.Second, 1)
	fake := adguardtest.New("dns1", "admin", "pw", entries)
	srv := fake.Server()
	defer srv.Close()

	c := New(config.Instance{Name: "dns1", URL: srv.URL, Username: "admin", Password: "pw"},
		&http.Client{Timeout: 2 * time.Second})

	// First page of 4.
	page1, err := c.QueryLog(context.Background(), QueryLogParams{Limit: 4})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Data) != 4 {
		t.Fatalf("page1 len = %d, want 4", len(page1.Data))
	}
	// Second page strictly older than the last item of page 1.
	cursor := page1.Data[3].RawTime
	page2, err := c.QueryLog(context.Background(), QueryLogParams{Limit: 4, OlderThan: cursor})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Data) == 0 {
		t.Fatal("page2 empty")
	}
	// No overlap: every page2 entry is strictly older than the cursor.
	for _, e := range page2.Data {
		if e.RawTime >= cursor {
			t.Errorf("page2 entry %q not strictly older than cursor %q", e.RawTime, cursor)
		}
	}
}

func TestFakeRejectsBadAuth(t *testing.T) {
	fake := adguardtest.New("dns1", "admin", "pw", nil)
	srv := fake.Server()
	defer srv.Close()

	c := New(config.Instance{Name: "dns1", URL: srv.URL, Username: "admin", Password: "wrong"},
		&http.Client{Timeout: 2 * time.Second})
	_, err := c.Status(context.Background())
	if err == nil {
		t.Fatal("expected auth error")
	}
}
