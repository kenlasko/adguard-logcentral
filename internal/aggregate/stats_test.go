package aggregate

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

// statsFake serves a fixed StatsPayload so merge math is exact and assertable.
func statsFake(t *testing.T, name string, payload adguardtest.StatsPayload) (*adguard.Client, func()) {
	t.Helper()
	fake := adguardtest.New(name, "u", "p", nil).WithStats(payload)
	srv := fake.Server()
	c := adguard.New(config.Instance{Name: name, URL: srv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})
	return c, srv.Close
}

func TestFetchStatsMergeAndWeightedAvg(t *testing.T) {
	p1 := adguardtest.StatsPayload{
		NumDNSQueries:       100,
		NumBlockedFiltering: 40,
		AvgProcessingTime:   0.010, // 10ms
		TopQueriedDomains:   []map[string]int64{{"a.com": 60}, {"b.com": 40}},
		TopBlockedDomains:   []map[string]int64{{"ads.net": 40}},
		TopClients:          []map[string]int64{{"1.1.1.1": 100}},
	}
	p2 := adguardtest.StatsPayload{
		NumDNSQueries:       300,
		NumBlockedFiltering: 60,
		AvgProcessingTime:   0.030, // 30ms
		TopQueriedDomains:   []map[string]int64{{"a.com": 50}, {"c.com": 20}},
		TopBlockedDomains:   []map[string]int64{{"ads.net": 10}, {"track.io": 5}},
		TopClients:          []map[string]int64{{"2.2.2.2": 300}},
	}
	c1, close1 := statsFake(t, "dns1", p1)
	defer close1()
	c2, close2 := statsFake(t, "dns2", p2)
	defer close2()

	m := FetchStats(context.Background(), []*adguard.Client{c1, c2})

	if m.NumDNSQueries != 400 || m.NumBlockedFiltering != 100 {
		t.Errorf("scalars = %d/%d, want 400/100", m.NumDNSQueries, m.NumBlockedFiltering)
	}
	// Weighted avg: (0.010*100 + 0.030*300)/400 = (1 + 9)/400 = 0.025.
	if diff := m.AvgProcessingTime - 0.025; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("weighted avg = %v, want 0.025", m.AvgProcessingTime)
	}
	// Blocked percent: 100/400 = 25%.
	if diff := m.BlockedPercent - 25.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("blocked percent = %v, want 25", m.BlockedPercent)
	}
	// a.com merged: 60+50 = 110, top of queried list.
	if m.TopQueriedDomains[0].Name != "a.com" || m.TopQueriedDomains[0].Count != 110 {
		t.Errorf("top queried = %+v, want a.com:110", m.TopQueriedDomains[0])
	}
	// Per-instance breakdown retained.
	if len(m.TopQueriedDomains[0].PerInstance) != 2 {
		t.Errorf("a.com breakdown = %+v, want 2 instances", m.TopQueriedDomains[0].PerInstance)
	}
	// ads.net merged: 40+10 = 50.
	if m.TopBlockedDomains[0].Name != "ads.net" || m.TopBlockedDomains[0].Count != 50 {
		t.Errorf("top blocked = %+v, want ads.net:50", m.TopBlockedDomains[0])
	}

	// Per-instance breakdown: one row per instance, in client order, each with
	// its own blocked share (dns1 40/100 = 40%, dns2 60/300 = 20%).
	if len(m.PerInstance) != 2 {
		t.Fatalf("per-instance rows = %d, want 2", len(m.PerInstance))
	}
	if m.PerInstance[0].Instance != "dns1" || m.PerInstance[1].Instance != "dns2" {
		t.Errorf("per-instance order = %q, %q, want dns1, dns2",
			m.PerInstance[0].Instance, m.PerInstance[1].Instance)
	}
	if m.PerInstance[0].NumDNSQueries != 100 || m.PerInstance[0].NumBlockedFiltering != 40 {
		t.Errorf("dns1 scalars = %+v, want 100/40", m.PerInstance[0])
	}
	if diff := m.PerInstance[0].BlockedPercent - 40.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("dns1 blocked percent = %v, want 40", m.PerInstance[0].BlockedPercent)
	}
	if diff := m.PerInstance[1].BlockedPercent - 20.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("dns2 blocked percent = %v, want 20", m.PerInstance[1].BlockedPercent)
	}
	if diff := m.PerInstance[1].AvgProcessingTime - 0.030; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("dns2 avg processing = %v, want 0.030", m.PerInstance[1].AvgProcessingTime)
	}
}

func TestFetchStatsPartialFailure(t *testing.T) {
	p := adguardtest.StatsPayload{NumDNSQueries: 10, NumBlockedFiltering: 2}
	c1, close1 := statsFake(t, "dns1", p)
	defer close1()

	failing := adguardtest.New("dns2", "u", "p", nil).WithFailures(adguardtest.Failures{Stats500: true})
	s2 := failing.Server()
	defer s2.Close()
	c2 := adguard.New(config.Instance{Name: "dns2", URL: s2.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})

	m := FetchStats(context.Background(), []*adguard.Client{c1, c2})
	if m.NumDNSQueries != 10 {
		t.Errorf("expected surviving instance totals, got %d", m.NumDNSQueries)
	}
	if len(m.Errors) != 1 || m.Errors[0].Instance != "dns2" {
		t.Errorf("expected dns2 error surfaced, got %+v", m.Errors)
	}
}
