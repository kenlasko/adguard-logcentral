package aggregate

import (
	"context"
	"sort"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
)

// InstanceCount is one instance's contribution to a merged top-N row.
type InstanceCount struct {
	Instance string
	Count    int64
}

// MergedTopEntry is a top-N row aggregated across instances, retaining the
// per-instance breakdown for the UI.
type MergedTopEntry struct {
	Name        string
	Count       int64
	PerInstance []InstanceCount
}

// MergedStats is the immutable result of merging /control/stats across
// instances: summed scalars, a query-weighted average processing time, and the
// three combined top-10 lists.
type MergedStats struct {
	NumDNSQueries       int64
	NumBlockedFiltering int64
	BlockedPercent      float64
	AvgProcessingTime   float64 // seconds, query-weighted across instances

	TopQueriedDomains []MergedTopEntry
	TopBlockedDomains []MergedTopEntry
	TopClients        []MergedTopEntry

	Errors []InstanceError
}

// topN caps a merged top list; ten matches the AdGuard UI.
const topN = 10

// FetchStats fans out /control/stats and merges the results per docs/DESIGN.md.
func FetchStats(ctx context.Context, clients []*adguard.Client) MergedStats {
	results := fanOut(ctx, clients, func(ctx context.Context, c *adguard.Client) (adguard.Stats, error) {
		return c.Stats(ctx)
	})

	merged := MergedStats{}
	var weightedSum float64 // sum of avg_processing_time * queries, for the weighted mean
	queried := newTopMerger()
	blocked := newTopMerger()
	clientsMerger := newTopMerger()

	for _, r := range results {
		if r.Err != nil {
			merged.Errors = append(merged.Errors, InstanceError{Instance: r.Instance, Err: r.Err})
			continue
		}
		s := r.Value
		merged.NumDNSQueries += s.NumDNSQueries
		merged.NumBlockedFiltering += s.NumBlockedFiltering
		weightedSum += s.AvgProcessingTime * float64(s.NumDNSQueries)
		queried.add(r.Instance, s.TopQueriedDomains)
		blocked.add(r.Instance, s.TopBlockedDomains)
		clientsMerger.add(r.Instance, s.TopClients)
	}

	if merged.NumDNSQueries > 0 {
		merged.AvgProcessingTime = weightedSum / float64(merged.NumDNSQueries)
		merged.BlockedPercent = float64(merged.NumBlockedFiltering) / float64(merged.NumDNSQueries) * 100
	}
	merged.TopQueriedDomains = queried.top(topN)
	merged.TopBlockedDomains = blocked.top(topN)
	merged.TopClients = clientsMerger.top(topN)
	return merged
}

// topMerger accumulates top-N entries across instances by key, retaining the
// per-instance breakdown.
type topMerger struct {
	totals      map[string]int64
	perInstance map[string][]InstanceCount
	order       []string // first-seen order, for deterministic tie-breaking input
}

func newTopMerger() *topMerger {
	return &topMerger{totals: map[string]int64{}, perInstance: map[string][]InstanceCount{}}
}

func (m *topMerger) add(instance string, entries []adguard.TopEntry) {
	for _, e := range entries {
		if _, seen := m.totals[e.Name]; !seen {
			m.order = append(m.order, e.Name)
		}
		m.totals[e.Name] += e.Count
		m.perInstance[e.Name] = append(m.perInstance[e.Name], InstanceCount{Instance: instance, Count: e.Count})
	}
}

// top returns the n highest-count entries, ties broken by name for stability.
func (m *topMerger) top(n int) []MergedTopEntry {
	entries := make([]MergedTopEntry, 0, len(m.totals))
	for _, name := range m.order {
		entries = append(entries, MergedTopEntry{
			Name:        name,
			Count:       m.totals[name],
			PerInstance: m.perInstance[name],
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Name < entries[j].Name
	})
	if len(entries) > n {
		entries = entries[:n]
	}
	return entries
}
