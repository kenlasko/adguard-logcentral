// Package adguard is a small client for the AdGuard Home control API. It
// exposes only the endpoints this aggregator needs: query log, stats, and
// status. Every call is stateless and authenticated with HTTP Basic Auth.
package adguard

import (
	"encoding/json"
	"fmt"
	"time"
)

// Question is the DNS question portion of a query log item.
type Question struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Class string `json:"class,omitempty"`
}

// ClientInfo is optional enrichment AdGuard attaches to a client. It may be
// null in the response, so every field is optional.
type ClientInfo struct {
	Name           string `json:"name,omitempty"`
	DisallowedRule string `json:"disallowed_rule,omitempty"`
	Disallowed     bool   `json:"disallowed,omitempty"`
}

// Rule is a single filtering rule that matched a query.
type Rule struct {
	FilterListID int64  `json:"filter_list_id"`
	Text         string `json:"text"`
}

// QueryLogItem is one entry in the query log.
//
// RawTime holds the exact "time" string from the API and must never be
// reformatted: it is fed back verbatim as the older_than pagination cursor,
// and reformatting risks nanosecond precision loss that would skip or
// duplicate entries at page boundaries. ParsedTime is derived for sorting.
type QueryLogItem struct {
	RawTime    string      `json:"time"`
	ParsedTime time.Time   `json:"-"`
	Client     string      `json:"client"`
	ClientInfo *ClientInfo `json:"client_info,omitempty"`
	Question   Question    `json:"question"`
	Reason     string      `json:"reason"`
	Rules      []Rule      `json:"rules"`
	Status     string      `json:"status,omitempty"`
	ElapsedMs  string      `json:"elapsedMs,omitempty"`
	Cached     bool        `json:"cached,omitempty"`
	Upstream   string      `json:"upstream,omitempty"`

	// Instance is populated by the aggregator, not the API, to tag which
	// AdGuard instance an entry came from. It is not part of the wire format.
	Instance string `json:"-"`
}

// QueryLogResponse is the envelope returned by /control/querylog.
type QueryLogResponse struct {
	Oldest string         `json:"oldest"`
	Data   []QueryLogItem `json:"data"`
}

// TopEntry is one ranked item in a stats top-N list.
type TopEntry struct {
	Name  string
	Count int64
}

// Stats mirrors the scalar counters and top-N lists from /control/stats.
// The top-N lists arrive as arrays of single-key objects, so they are decoded
// through UnmarshalJSON into ordered TopEntry slices.
type Stats struct {
	NumDNSQueries           int64   `json:"num_dns_queries"`
	NumBlockedFiltering     int64   `json:"num_blocked_filtering"`
	NumReplacedSafebrowsing int64   `json:"num_replaced_safebrowsing"`
	NumReplacedSafesearch   int64   `json:"num_replaced_safesearch"`
	NumReplacedParental     int64   `json:"num_replaced_parental"`
	AvgProcessingTime       float64 `json:"avg_processing_time"`

	TopQueriedDomains []TopEntry `json:"-"`
	TopBlockedDomains []TopEntry `json:"-"`
	TopClients        []TopEntry `json:"-"`
}

// statsWire is the raw shape of /control/stats before top-N normalization.
type statsWire struct {
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

// UnmarshalJSON decodes the wire shape and normalizes the top-N arrays of
// single-key maps into ordered TopEntry slices, preserving API order.
func (s *Stats) UnmarshalJSON(data []byte) error {
	var w statsWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*s = Stats{
		NumDNSQueries:           w.NumDNSQueries,
		NumBlockedFiltering:     w.NumBlockedFiltering,
		NumReplacedSafebrowsing: w.NumReplacedSafebrowsing,
		NumReplacedSafesearch:   w.NumReplacedSafesearch,
		NumReplacedParental:     w.NumReplacedParental,
		AvgProcessingTime:       w.AvgProcessingTime,
		TopQueriedDomains:       toTopEntries(w.TopQueriedDomains),
		TopBlockedDomains:       toTopEntries(w.TopBlockedDomains),
		TopClients:              toTopEntries(w.TopClients),
	}
	return nil
}

// toTopEntries flattens AdGuard's array of single-key objects into ordered
// TopEntry values. Each object is expected to hold exactly one key.
func toTopEntries(raw []map[string]int64) []TopEntry {
	entries := make([]TopEntry, 0, len(raw))
	for _, m := range raw {
		for name, count := range m {
			entries = append(entries, TopEntry{Name: name, Count: count})
		}
	}
	return entries
}

// Status mirrors /control/status, used as a lightweight health probe.
type Status struct {
	Running           bool   `json:"running"`
	Version           string `json:"version"`
	ProtectionEnabled bool   `json:"protection_enabled"`
}

// APIError is returned when the AdGuard API responds with a non-2xx status.
type APIError struct {
	Instance string
	Path     string
	Status   int
	Body     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("adguard %q: %s returned HTTP %d: %s", e.Instance, e.Path, e.Status, e.Body)
}
