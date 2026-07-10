package aggregate

import (
	"slices"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
)

// taggedEntries pairs an instance name with the entries fetched from it.
type taggedEntries struct {
	Instance string
	Entries  []adguard.QueryLogItem
}

// mergeResult is the outcome of merging one page across instances.
type mergeResult struct {
	// Page is the merged, sorted, trimmed slice of entries for this page.
	Page []adguard.QueryLogItem
	// Consumed maps instance name to how many of its entries were taken into
	// Page, so the caller can advance each instance's cursor correctly.
	Consumed map[string]int
}

// mergeDescending merges per-instance entry slices into a single page sorted
// newest-first, trimmed to pageSize. Ties (identical ParsedTime) break
// deterministically by instance name then RawTime, so pagination is stable.
// Inputs are never mutated; a new slice is returned.
func mergeDescending(groups []taggedEntries, pageSize int) mergeResult {
	total := 0
	for _, g := range groups {
		total += len(g.Entries)
	}
	combined := make([]adguard.QueryLogItem, 0, total)
	for _, g := range groups {
		combined = append(combined, g.Entries...)
	}

	slices.SortStableFunc(combined, func(a, b adguard.QueryLogItem) int {
		// Newest first: reverse chronological.
		if c := b.ParsedTime.Compare(a.ParsedTime); c != 0 {
			return c
		}
		if a.Instance != b.Instance {
			if a.Instance < b.Instance {
				return -1
			}
			return 1
		}
		// RawTime tie-break keeps identical-nanosecond entries ordered.
		if a.RawTime < b.RawTime {
			return -1
		}
		if a.RawTime > b.RawTime {
			return 1
		}
		return 0
	})

	if len(combined) > pageSize {
		combined = combined[:pageSize]
	}

	consumed := make(map[string]int, len(groups))
	for _, item := range combined {
		consumed[item.Instance]++
	}
	return mergeResult{Page: combined, Consumed: consumed}
}
