// Package adguardtest provides an in-memory fake of the AdGuard Home control
// API. It is the shared fixture for every test layer above the client, and it
// also backs cmd/fakeadguard for manual end-to-end runs. It implements real
// older_than/limit/search/response_status semantics, enforces Basic Auth, and
// supports failure injection so error paths can be exercised deterministically.
package adguardtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Entry is one seeded query log record. Times are stored as RFC3339Nano
// strings so the fake round-trips the exact cursor format the real API uses.
type Entry struct {
	Time     string
	Client   string
	Domain   string
	QType    string
	Reason   string
	Status   string
	Blocked  bool
	Rewrite  bool
	RuleText string
	Elapsed  string
}

// Failures toggles error injection per endpoint.
type Failures struct {
	QueryLog500 bool
	Stats500    bool
	Status500   bool
	Unauthate   bool          // force 401 on every endpoint
	Hang        time.Duration // sleep before responding to querylog (to trip client timeouts)
}

// Fake is a configurable fake AdGuard instance. Construct it with New, then
// mount it with Server() (tests) or Handler() (cmd/fakeadguard).
type Fake struct {
	Name     string
	Username string
	Password string

	entries  []Entry
	stats    StatsPayload
	status   StatusPayload
	failures Failures

	// rules holds the mutable custom filtering rules. It is a pointer so the
	// value-copying With* helpers share one store (and one lock) with the
	// original rather than copying a mutex.
	rules *ruleStore
}

// ruleStore is the concurrency-safe backing for a fake's custom filtering
// rules, mutated by /control/filtering/set_rules and read by /status.
type ruleStore struct {
	mu    sync.Mutex
	rules []string
}

func (rs *ruleStore) get() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.rules...)
}

func (rs *ruleStore) set(rules []string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.rules = append([]string(nil), rules...)
}

// StatsPayload is the response shape for /control/stats.
type StatsPayload struct {
	NumDNSQueries           int64              `json:"num_dns_queries"`
	NumBlockedFiltering     int64              `json:"num_blocked_filtering"`
	NumReplacedSafebrowsing int64              `json:"num_replaced_safebrowsing"`
	NumReplacedSafesearch   int64              `json:"num_replaced_safesearch"`
	NumReplacedParental     int64              `json:"num_replaced_parental"`
	AvgProcessingTime       float64            `json:"avg_processing_time"`
	TopQueriedDomains       []map[string]int64 `json:"top_queried_domains"`
	TopBlockedDomains       []map[string]int64 `json:"top_blocked_domains"`
	TopClients              []map[string]int64 `json:"top_clients"`
}

// StatusPayload is the response shape for /control/status.
type StatusPayload struct {
	Running           bool   `json:"running"`
	Version           string `json:"version"`
	ProtectionEnabled bool   `json:"protection_enabled"`
}

// New builds a Fake with the given credentials and seeded entries. Entries are
// stored sorted newest-first to match AdGuard's query log ordering.
func New(name, username, password string, entries []Entry) *Fake {
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Time > sorted[j].Time // RFC3339Nano strings sort lexically by time
	})
	f := &Fake{
		Name:     name,
		Username: username,
		Password: password,
		entries:  sorted,
		status:   StatusPayload{Running: true, Version: "v0.107.fake", ProtectionEnabled: true},
		rules:    &ruleStore{},
	}
	f.stats = deriveStats(sorted)
	return f
}

// Rules returns a copy of the fake's current custom filtering rules, for tests
// that assert what block/unblock actions persisted.
func (f *Fake) Rules() []string { return f.rules.get() }

// WithFailures returns a copy of the fake with failure injection applied.
func (f *Fake) WithFailures(fl Failures) *Fake {
	clone := *f
	clone.failures = fl
	return &clone
}

// WithStatus overrides the status payload (for health-bar tests).
func (f *Fake) WithStatus(s StatusPayload) *Fake {
	clone := *f
	clone.status = s
	return &clone
}

// WithStats overrides the derived stats payload with an explicit one, so stats
// merge math can be asserted against exact numbers.
func (f *Fake) WithStats(s StatsPayload) *Fake {
	clone := *f
	clone.stats = s
	return &clone
}

// Server starts an httptest.Server serving this fake. Callers close it.
func (f *Fake) Server() *httptest.Server {
	return httptest.NewServer(f.Handler())
}

// Handler returns the http.Handler implementing the control API.
func (f *Fake) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /control/querylog", f.handleQueryLog)
	mux.HandleFunc("GET /control/stats", f.handleStats)
	mux.HandleFunc("GET /control/status", f.handleStatus)
	mux.HandleFunc("GET /control/filtering/status", f.handleFilteringStatus)
	mux.HandleFunc("POST /control/filtering/set_rules", f.handleSetRules)
	return mux
}

func (f *Fake) authOK(r *http.Request) bool {
	if f.failures.Unauthate {
		return false
	}
	u, p, ok := r.BasicAuth()
	return ok && u == f.Username && p == f.Password
}

func (f *Fake) handleQueryLog(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if f.failures.QueryLog500 {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if f.failures.Hang > 0 {
		time.Sleep(f.failures.Hang)
	}

	q := r.URL.Query()
	olderThan := q.Get("older_than")
	search := strings.ToLower(q.Get("search"))
	respStatus := q.Get("response_status")
	limit := 0
	if v := q.Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}

	filtered := make([]Entry, 0, len(f.entries))
	for _, e := range f.entries {
		if olderThan != "" && e.Time >= olderThan {
			continue // strictly older than the cursor
		}
		if search != "" && !strings.Contains(strings.ToLower(e.Domain), search) &&
			!strings.Contains(strings.ToLower(e.Client), search) {
			continue
		}
		if !matchesStatus(e, respStatus) {
			continue
		}
		filtered = append(filtered, e)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	oldest := ""
	if len(filtered) > 0 {
		oldest = filtered[len(filtered)-1].Time
	}
	writeJSON(w, map[string]any{
		"oldest": oldest,
		"data":   toWireItems(filtered),
	})
}

func (f *Fake) handleStats(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if f.failures.Stats500 {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	writeJSON(w, f.stats)
}

func (f *Fake) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if f.failures.Status500 {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	writeJSON(w, f.status)
}

func (f *Fake) handleFilteringStatus(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{
		"enabled":    true,
		"user_rules": f.rules.get(),
	})
}

func (f *Fake) handleSetRules(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Rules []string `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.rules.set(body.Rules)
	writeJSON(w, map[string]any{})
}

// matchesStatus applies a subset of AdGuard response_status filter semantics
// sufficient for tests: all/"" pass everything, blocked/filtered require a
// block, rewritten requires a rewrite, processed excludes blocks.
func matchesStatus(e Entry, status string) bool {
	switch status {
	case "", "all":
		return true
	case "blocked", "filtered":
		return e.Blocked
	case "rewritten":
		return e.Rewrite
	case "processed":
		return !e.Blocked
	default:
		return true
	}
}

func toWireItems(entries []Entry) []map[string]any {
	items := make([]map[string]any, len(entries))
	for i, e := range entries {
		rules := []map[string]any{}
		if e.RuleText != "" {
			rules = append(rules, map[string]any{"filter_list_id": 1, "text": e.RuleText})
		}
		items[i] = map[string]any{
			"time":      e.Time,
			"client":    e.Client,
			"question":  map[string]any{"name": e.Domain, "type": e.QType, "class": "IN"},
			"reason":    e.Reason,
			"status":    e.Status,
			"rules":     rules,
			"elapsedMs": e.Elapsed,
			"cached":    false,
			"upstream":  "https://dns.example:853",
		}
	}
	return items
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// deriveStats builds a plausible stats payload from the seeded entries so the
// fake's /stats output is internally consistent with its query log.
func deriveStats(entries []Entry) StatsPayload {
	queried := map[string]int64{}
	blocked := map[string]int64{}
	clients := map[string]int64{}
	var numBlocked int64
	var elapsedSum float64
	var elapsedCount int64

	for _, e := range entries {
		queried[e.Domain]++
		clients[e.Client]++
		if e.Blocked {
			numBlocked++
			blocked[e.Domain]++
		}
		if ms, err := strconv.ParseFloat(e.Elapsed, 64); err == nil {
			elapsedSum += ms
			elapsedCount++
		}
	}

	avg := 0.0
	if elapsedCount > 0 {
		avg = elapsedSum / float64(elapsedCount) / 1000.0 // AdGuard reports seconds
	}
	return StatsPayload{
		NumDNSQueries:       int64(len(entries)),
		NumBlockedFiltering: numBlocked,
		AvgProcessingTime:   avg,
		TopQueriedDomains:   topN(queried, 10),
		TopBlockedDomains:   topN(blocked, 10),
		TopClients:          topN(clients, 10),
	}
}

// topN sorts a name->count map descending (ties broken by name for
// determinism) and returns the top n as single-key maps, matching the API.
func topN(counts map[string]int64, n int) []map[string]int64 {
	type kv struct {
		name  string
		count int64
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].name < pairs[j].name
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	out := make([]map[string]int64, len(pairs))
	for i, p := range pairs {
		out[i] = map[string]int64{p.name: p.count}
	}
	return out
}

// Generate builds count deterministic entries ending at endTime and going
// backwards at the given step. The seed varies domains/clients/reasons so the
// data looks realistic without any randomness (reproducible across runs).
func Generate(count int, endTime time.Time, step time.Duration, seed int) []Entry {
	domains := []string{"example.com", "ads.tracker.net", "cdn.assets.io", "telemetry.vendor.com", "news.site.org", "api.service.dev"}
	clients := []string{"192.168.1.10", "192.168.1.20", "192.168.1.30", "10.0.0.5"}
	qtypes := []string{"A", "AAAA", "HTTPS", "PTR"}

	entries := make([]Entry, count)
	for i := 0; i < count; i++ {
		// Nanosecond offset keeps timestamps strictly unique and ordered.
		t := endTime.Add(-time.Duration(i) * step).Add(time.Duration((seed+i)%1000) * time.Nanosecond)
		domain := domains[(seed+i)%len(domains)]
		blocked := strings.Contains(domain, "ads") || strings.Contains(domain, "tracker") || strings.Contains(domain, "telemetry")
		reason := "NotFilteredNotFound"
		status := "processed"
		rule := ""
		if blocked {
			reason = "FilteredBlackList"
			status = "blocked"
			rule = "||" + domain + "^"
		}
		entries[i] = Entry{
			Time:     t.Format(time.RFC3339Nano),
			Client:   clients[(seed+i)%len(clients)],
			Domain:   domain,
			QType:    qtypes[(seed+i)%len(qtypes)],
			Reason:   reason,
			Status:   status,
			Blocked:  blocked,
			RuleText: rule,
			Elapsed:  fmt.Sprintf("%d", 1+(seed+i)%40),
		}
	}
	return entries
}
