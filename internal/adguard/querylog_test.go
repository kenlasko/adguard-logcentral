package adguard

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

func fakeClient(t *testing.T, entries []adguardtest.Entry) (*Client, func()) {
	t.Helper()
	fake := adguardtest.New("dns1", "admin", "pw", entries)
	srv := fake.Server()
	c := New(config.Instance{Name: "dns1", URL: srv.URL, Username: "admin", Password: "pw"},
		&http.Client{Timeout: 2 * time.Second})
	return c, srv.Close
}

func TestQueryLogSearchFilter(t *testing.T) {
	entries := []adguardtest.Entry{
		{Time: "2026-07-10T00:00:03Z", Client: "1.1.1.1", Domain: "ads.tracker.net", QType: "A", Blocked: true, Reason: "FilteredBlackList", Status: "blocked"},
		{Time: "2026-07-10T00:00:02Z", Client: "1.1.1.1", Domain: "example.com", QType: "A"},
		{Time: "2026-07-10T00:00:01Z", Client: "2.2.2.2", Domain: "cdn.example.com", QType: "A"},
	}
	c, cleanup := fakeClient(t, entries)
	defer cleanup()

	resp, err := c.QueryLog(context.Background(), QueryLogParams{Search: "example"})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("search example returned %d, want 2", len(resp.Data))
	}
	for _, e := range resp.Data {
		if e.Question.Name != "example.com" && e.Question.Name != "cdn.example.com" {
			t.Errorf("unexpected match: %q", e.Question.Name)
		}
	}
}

func TestQueryLogResponseStatusFilter(t *testing.T) {
	entries := []adguardtest.Entry{
		{Time: "2026-07-10T00:00:03Z", Client: "1.1.1.1", Domain: "ads.net", QType: "A", Blocked: true, Reason: "FilteredBlackList", Status: "blocked"},
		{Time: "2026-07-10T00:00:02Z", Client: "1.1.1.1", Domain: "example.com", QType: "A"},
	}
	c, cleanup := fakeClient(t, entries)
	defer cleanup()

	resp, err := c.QueryLog(context.Background(), QueryLogParams{ResponseStatus: "blocked"})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Question.Name != "ads.net" {
		t.Fatalf("blocked filter returned %+v", resp.Data)
	}
}

func TestQueryLogInjected500(t *testing.T) {
	fake := adguardtest.New("dns1", "admin", "pw", nil).WithFailures(adguardtest.Failures{QueryLog500: true})
	srv := fake.Server()
	defer srv.Close()
	c := New(config.Instance{Name: "dns1", URL: srv.URL, Username: "admin", Password: "pw"},
		&http.Client{Timeout: 2 * time.Second})

	if _, err := c.QueryLog(context.Background(), QueryLogParams{}); err == nil {
		t.Fatal("expected error from injected 500")
	}
}

func TestQueryLogHangTripsTimeout(t *testing.T) {
	fake := adguardtest.New("dns1", "admin", "pw", nil).WithFailures(adguardtest.Failures{Hang: 200 * time.Millisecond})
	srv := fake.Server()
	defer srv.Close()
	c := New(config.Instance{Name: "dns1", URL: srv.URL, Username: "admin", Password: "pw"},
		&http.Client{Timeout: 50 * time.Millisecond})

	if _, err := c.QueryLog(context.Background(), QueryLogParams{}); err == nil {
		t.Fatal("expected timeout error from hang")
	}
}
