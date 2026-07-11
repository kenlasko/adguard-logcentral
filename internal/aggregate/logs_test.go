package aggregate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

// toggleServer wraps a fake whose reachability can be flipped at runtime, so
// tests can take an instance "down" and bring it back.
type toggleServer struct {
	mu   sync.Mutex
	down bool
	srv  *httptest.Server
}

func newToggle(fake *adguardtest.Fake) *toggleServer {
	ts := &toggleServer{}
	inner := fake.Handler()
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		down := ts.down
		ts.mu.Unlock()
		if down {
			http.Error(w, "instance down", http.StatusInternalServerError)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	return ts
}

func (ts *toggleServer) setDown(v bool) {
	ts.mu.Lock()
	ts.down = v
	ts.mu.Unlock()
}

func (ts *toggleServer) close() { ts.srv.Close() }

func entry(raw, domain string) adguardtest.Entry {
	return adguardtest.Entry{Time: raw, Client: "1.1.1.1", Domain: domain, QType: "A", Reason: "x", Status: "processed"}
}

func clientFor(name string, url string) *adguard.Client {
	return adguard.New(config.Instance{Name: name, URL: url, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})
}

// paginateAll drives FetchLogs until exhausted, returning the full ordered
// sequence of RawTimes plus the number of pages fetched.
func paginateAll(t *testing.T, clients []*adguard.Client, filter Filter, pageSize int) ([]string, int) {
	t.Helper()
	var seq []string
	var cursor *Cursor
	pages := 0
	for {
		page := FetchLogs(context.Background(), clients, filter, cursor, pageSize)
		pages++
		for _, e := range page.Entries {
			seq = append(seq, e.RawTime)
		}
		if !page.HasMore {
			break
		}
		cursor = DecodeCursor(page.NextCursor)
		if pages > 100 {
			t.Fatal("pagination did not terminate")
		}
	}
	return seq, pages
}

// TestScenarioInterleavedPagination is mandatory scenario (a): three instances
// with interleaved timestamps paginate across multiple pages with zero loss and
// zero duplication, in exact global order.
func TestScenarioInterleavedPagination(t *testing.T) {
	f1 := adguardtest.New("dns1", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:09Z", "a.com"),
		entry("2026-07-10T00:00:06Z", "d.com"),
		entry("2026-07-10T00:00:03Z", "g.com"),
	})
	f2 := adguardtest.New("dns2", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:08Z", "b.com"),
		entry("2026-07-10T00:00:05Z", "e.com"),
		entry("2026-07-10T00:00:02Z", "h.com"),
	})
	f3 := adguardtest.New("dns3", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:07Z", "c.com"),
		entry("2026-07-10T00:00:04Z", "f.com"),
		entry("2026-07-10T00:00:01Z", "i.com"),
	})
	s1, s2, s3 := f1.Server(), f2.Server(), f3.Server()
	defer s1.Close()
	defer s2.Close()
	defer s3.Close()

	clients := []*adguard.Client{
		clientFor("dns1", s1.URL),
		clientFor("dns2", s2.URL),
		clientFor("dns3", s3.URL),
	}

	seq, pages := paginateAll(t, clients, Filter{}, 2)
	want := []string{
		"2026-07-10T00:00:09Z", "2026-07-10T00:00:08Z", "2026-07-10T00:00:07Z",
		"2026-07-10T00:00:06Z", "2026-07-10T00:00:05Z", "2026-07-10T00:00:04Z",
		"2026-07-10T00:00:03Z", "2026-07-10T00:00:02Z", "2026-07-10T00:00:01Z",
	}
	if len(seq) != len(want) {
		t.Fatalf("got %d entries across %d pages, want %d: %v", len(seq), pages, len(want), seq)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Errorf("position %d = %q, want %q", i, seq[i], want[i])
		}
	}
	if pages < 3 {
		t.Errorf("expected 3+ pages, got %d", pages)
	}
	// Zero duplication: all entries unique.
	seen := map[string]bool{}
	for _, r := range seq {
		if seen[r] {
			t.Errorf("duplicate entry %q", r)
		}
		seen[r] = true
	}
}

// TestScenarioInstanceDownThenRecovers is mandatory scenario (b): a down
// instance yields a partial page, its error is surfaced, its cursor is
// unchanged, and a retry after recovery resumes correctly with no loss.
func TestScenarioInstanceDownThenRecovers(t *testing.T) {
	f1 := adguardtest.New("dns1", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:09Z", "a.com"),
		entry("2026-07-10T00:00:06Z", "d.com"),
	})
	f2 := adguardtest.New("dns2", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:08Z", "b.com"),
		entry("2026-07-10T00:00:05Z", "e.com"),
	})
	s1 := f1.Server()
	defer s1.Close()
	t2 := newToggle(f2)
	defer t2.close()

	clients := []*adguard.Client{
		clientFor("dns1", s1.URL),
		clientFor("dns2", t2.srv.URL),
	}

	// Page 1 with dns2 down: only dns1 entries, dns2 error surfaced.
	t2.setDown(true)
	page1 := FetchLogs(context.Background(), clients, Filter{}, nil, 2)
	if len(page1.Errors) != 1 || page1.Errors[0].Instance != "dns2" {
		t.Fatalf("expected dns2 error, got %+v", page1.Errors)
	}
	for _, e := range page1.Entries {
		if e.Instance != "dns1" {
			t.Errorf("page1 unexpectedly served %s entry while dns2 down", e.Instance)
		}
	}
	if !page1.HasMore {
		t.Fatal("HasMore should be true while dns2 is down (unfinished)")
	}
	cur := DecodeCursor(page1.NextCursor)
	// dns2 cursor must be unchanged (still empty O, not done) so it retries.
	if cur.I["dns2"].O != "" || cur.I["dns2"].D {
		t.Errorf("dns2 cursor should be unchanged after failure, got %+v", cur.I["dns2"])
	}

	// Recover dns2 and drain. The full set must appear exactly once each.
	t2.setDown(false)
	var seq []string
	for _, e := range page1.Entries {
		seq = append(seq, e.RawTime)
	}
	cursor := cur
	for {
		page := FetchLogs(context.Background(), clients, Filter{}, cursor, 2)
		for _, e := range page.Entries {
			seq = append(seq, e.RawTime)
		}
		if !page.HasMore {
			break
		}
		cursor = DecodeCursor(page.NextCursor)
	}
	want := map[string]bool{
		"2026-07-10T00:00:09Z": true, "2026-07-10T00:00:08Z": true,
		"2026-07-10T00:00:06Z": true, "2026-07-10T00:00:05Z": true,
	}
	if len(seq) != len(want) {
		t.Fatalf("after recovery got %d entries, want %d: %v", len(seq), len(want), seq)
	}
	for _, r := range seq {
		if !want[r] {
			t.Errorf("unexpected or duplicate entry %q", r)
		}
		delete(want, r)
	}
}

// TestScenarioInstanceExhaustsMidRun is mandatory scenario (c): a small
// instance exhausts before a larger one, is marked done, and is never queried
// again (verified by its request count freezing).
func TestScenarioInstanceExhaustsMidRun(t *testing.T) {
	// dns1 has one entry; dns2 has three. With pageSize 2, dns1 exhausts on
	// page 1 and must not be hit again.
	f1 := adguardtest.New("dns1", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:10Z", "a.com"),
	})
	f2 := adguardtest.New("dns2", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:09Z", "b.com"),
		entry("2026-07-10T00:00:08Z", "c.com"),
		entry("2026-07-10T00:00:07Z", "d.com"),
	})

	var dns1Hits int
	var mu sync.Mutex
	inner1 := f1.Handler()
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		dns1Hits++
		mu.Unlock()
		inner1.ServeHTTP(w, r)
	}))
	defer s1.Close()
	s2 := f2.Server()
	defer s2.Close()

	clients := []*adguard.Client{clientFor("dns1", s1.URL), clientFor("dns2", s2.URL)}

	// Page 1.
	page1 := FetchLogs(context.Background(), clients, Filter{}, nil, 2)
	cur := DecodeCursor(page1.NextCursor)
	if !cur.I["dns1"].D {
		t.Fatalf("dns1 should be done after page 1, cursor: %+v", cur.I["dns1"])
	}
	mu.Lock()
	hitsAfterPage1 := dns1Hits
	mu.Unlock()

	// Drain the rest; dns1 must not be queried again.
	cursor := cur
	for cursor != nil {
		page := FetchLogs(context.Background(), clients, Filter{}, cursor, 2)
		if !page.HasMore {
			break
		}
		cursor = DecodeCursor(page.NextCursor)
	}
	mu.Lock()
	finalHits := dns1Hits
	mu.Unlock()
	if finalHits != hitsAfterPage1 {
		t.Errorf("dns1 queried again after exhaustion: %d -> %d", hitsAfterPage1, finalHits)
	}
}

// TestScenarioAllDone is mandatory scenario (d): when everything is served,
// HasMore is false and no cursor (sentinel) is emitted.
func TestScenarioAllDone(t *testing.T) {
	f1 := adguardtest.New("dns1", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:02Z", "a.com"),
	})
	f2 := adguardtest.New("dns2", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:01Z", "b.com"),
	})
	s1, s2 := f1.Server(), f2.Server()
	defer s1.Close()
	defer s2.Close()
	clients := []*adguard.Client{clientFor("dns1", s1.URL), clientFor("dns2", s2.URL)}

	page := FetchLogs(context.Background(), clients, Filter{}, nil, 10)
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Error("HasMore should be false when all instances fit in one page")
	}
	if page.NextCursor != "" {
		t.Errorf("no sentinel cursor expected, got %q", page.NextCursor)
	}
}

// TestFetchLogsExactSearch verifies that a search without a wildcard returns
// only exact domain matches, excluding near-miss substrings, even when they
// span multiple pages (the cursor must scan past the non-matching entries).
func TestFetchLogsExactSearch(t *testing.T) {
	f1 := adguardtest.New("dns1", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:05Z", "192.168.1.20"),
		entry("2026-07-10T00:00:04Z", "192.168.1.2"),
		entry("2026-07-10T00:00:03Z", "192.168.1.21"),
		entry("2026-07-10T00:00:02Z", "192.168.1.2"),
		entry("2026-07-10T00:00:01Z", "192.168.1.200"),
	})
	s1 := f1.Server()
	defer s1.Close()
	clients := []*adguard.Client{clientFor("dns1", s1.URL)}

	// Exact search: only the two "192.168.1.2" entries, none of the near-misses.
	seq, _ := paginateAll(t, clients, Filter{Search: "192.168.1.2"}, 2)
	want := []string{"2026-07-10T00:00:04Z", "2026-07-10T00:00:02Z"}
	if len(seq) != len(want) {
		t.Fatalf("exact search got %d entries, want %d: %v", len(seq), len(want), seq)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Errorf("position %d = %q, want %q", i, seq[i], want[i])
		}
	}
}

// TestFetchLogsWildcardSearch verifies that a trailing "*" matches by prefix,
// including the near-miss entries excluded by an exact search.
func TestFetchLogsWildcardSearch(t *testing.T) {
	f1 := adguardtest.New("dns1", "u", "p", []adguardtest.Entry{
		entry("2026-07-10T00:00:05Z", "192.168.1.20"),
		entry("2026-07-10T00:00:04Z", "192.168.1.2"),
		entry("2026-07-10T00:00:03Z", "192.168.1.21"),
		entry("2026-07-10T00:00:02Z", "10.0.0.1"),
	})
	s1 := f1.Server()
	defer s1.Close()
	clients := []*adguard.Client{clientFor("dns1", s1.URL)}

	seq, _ := paginateAll(t, clients, Filter{Search: "192.168.1.2*"}, 2)
	want := map[string]bool{
		"2026-07-10T00:00:05Z": true,
		"2026-07-10T00:00:04Z": true,
		"2026-07-10T00:00:03Z": true,
	}
	if len(seq) != len(want) {
		t.Fatalf("wildcard search got %d entries, want %d: %v", len(seq), len(want), seq)
	}
	for _, r := range seq {
		if !want[r] {
			t.Errorf("unexpected entry %q", r)
		}
	}
}

// TestFetchLogsRespectsInstanceFilter verifies unchecking an instance removes
// its rows entirely.
func TestFetchLogsRespectsInstanceFilter(t *testing.T) {
	f1 := adguardtest.New("dns1", "u", "p", []adguardtest.Entry{entry("2026-07-10T00:00:02Z", "a.com")})
	f2 := adguardtest.New("dns2", "u", "p", []adguardtest.Entry{entry("2026-07-10T00:00:01Z", "b.com")})
	s1, s2 := f1.Server(), f2.Server()
	defer s1.Close()
	defer s2.Close()
	clients := []*adguard.Client{clientFor("dns1", s1.URL), clientFor("dns2", s2.URL)}

	page := FetchLogs(context.Background(), clients, Filter{Instances: []string{"dns1"}}, nil, 10)
	for _, e := range page.Entries {
		if e.Instance != "dns1" {
			t.Errorf("filter should exclude %s", e.Instance)
		}
	}
	if len(page.Entries) != 1 {
		t.Errorf("expected only dns1 entry, got %d", len(page.Entries))
	}
}
