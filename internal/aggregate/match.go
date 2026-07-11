package aggregate

import (
	"strings"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
)

// Search matching is exact by default: a search of "192.168.1.2" matches only
// the client or domain "192.168.1.2", never "192.168.1.20". A "*" in the search
// is a wildcard matching any run of characters (including none), so
// "192.168.1.2*" matches "192.168.1.2", "192.168.1.20", "192.168.1.21", and so
// on. Matching is case-insensitive and anchored to the whole value.
//
// A pattern is matched against three values on each entry: the queried domain,
// the client address, and the client's friendly name (the name AdGuard shows in
// brackets beside the address), so an operator can search by whichever they
// know.
//
// AdGuard's own /querylog search is an unanchored substring match, which cannot
// express "exact only". We therefore treat AdGuard's search purely as a coarse
// pre-filter (see adguardSearchTerm) that narrows the fetched candidates, and
// apply the precise anchored/wildcard match ourselves before display. The
// pre-filter never drops a real match: AdGuard's search matches the same domain,
// client address, and client name we match here, and every match necessarily
// contains each literal (non-"*") segment of the pattern as a substring, so
// sending any one such segment to AdGuard only widens, never narrows, the true
// result set.

// matchesSearch reports whether an entry satisfies the filter's search pattern,
// matching against the queried domain, the client address, or the client's
// friendly name. An empty pattern matches everything.
func (f Filter) matchesSearch(item adguard.QueryLogItem) bool {
	if f.Search == "" {
		return true
	}
	if matchGlob(f.Search, item.Question.Name) || matchGlob(f.Search, item.Client) {
		return true
	}
	return item.ClientInfo != nil && matchGlob(f.Search, item.ClientInfo.Name)
}

// filterEntries returns a new slice containing only the entries that match the
// filter's search pattern. Inputs are never mutated. When the pattern is empty
// the original slice is returned unchanged (no allocation).
func (f Filter) filterEntries(entries []adguard.QueryLogItem) []adguard.QueryLogItem {
	if f.Search == "" {
		return entries
	}
	out := make([]adguard.QueryLogItem, 0, len(entries))
	for _, e := range entries {
		if f.matchesSearch(e) {
			out = append(out, e)
		}
	}
	return out
}

// matchGlob reports whether s matches pattern. With no "*", the match is exact
// (case-insensitive full-string equality). With "*", each "*" matches any run of
// characters, including an empty one, and the match is anchored to both ends.
func matchGlob(pattern, s string) bool {
	pattern = strings.ToLower(pattern)
	s = strings.ToLower(s)

	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s // no wildcard: exact match only
	}

	// The first segment must be a literal prefix.
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]

	// The last segment must be a literal suffix.
	last := parts[len(parts)-1]
	if !strings.HasSuffix(s, last) {
		return false
	}
	s = s[:len(s)-len(last)]

	// Interior segments must appear in order within what remains.
	for _, part := range parts[1 : len(parts)-1] {
		idx := strings.Index(s, part)
		if idx < 0 {
			return false
		}
		s = s[idx+len(part):]
	}
	return true
}

// adguardSearchTerm reduces a user pattern to the coarse substring passed to
// AdGuard's /querylog search. With no wildcard the whole pattern is used. With
// wildcards the longest literal segment is used, since every true match must
// contain it as a substring; an all-"*" pattern yields "", i.e. no pre-filter.
func adguardSearchTerm(pattern string) string {
	if !strings.Contains(pattern, "*") {
		return pattern
	}
	longest := ""
	for _, part := range strings.Split(pattern, "*") {
		if len(part) > len(longest) {
			longest = part
		}
	}
	return longest
}
