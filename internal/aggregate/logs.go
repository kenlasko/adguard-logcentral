package aggregate

import (
	"context"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
)

// Filter is the set of user-selected query log filters. Instances is the list
// of selected instance names; an empty Instances means "all instances".
type Filter struct {
	Search    string
	Status    string
	Instances []string
}

// selected reports whether an instance name is included by this filter.
func (f Filter) selected(name string) bool {
	if len(f.Instances) == 0 {
		return true
	}
	for _, n := range f.Instances {
		if n == name {
			return true
		}
	}
	return false
}

// InstanceError names an instance whose fetch failed for this page, so the UI
// can show a partial-results banner without failing the whole request.
type InstanceError struct {
	Instance string
	Err      error
}

// LogsPage is one immutable page of merged query log entries.
type LogsPage struct {
	Entries    []adguard.QueryLogItem
	NextCursor string
	HasMore    bool
	Errors     []InstanceError
}

// FetchLogs fetches one page of merged query log entries across the selected,
// non-exhausted instances, following the composite-cursor algorithm described
// in docs/DESIGN.md. It never mutates its inputs.
func FetchLogs(ctx context.Context, clients []*adguard.Client, filter Filter, cursor *Cursor, pageSize int) LogsPage {
	prev := map[string]InstanceCursor{}
	if cursor != nil {
		prev = cursor.I
	}

	// Partition clients into those we must query this page and those already done.
	active := make([]*adguard.Client, 0, len(clients))
	for _, c := range clients {
		if !filter.selected(c.Name()) {
			continue
		}
		if prev[c.Name()].D {
			continue // exhausted on an earlier page
		}
		active = append(active, c)
	}

	results := fanOut(ctx, active, func(ctx context.Context, c *adguard.Client) (adguard.QueryLogResponse, error) {
		return c.QueryLog(ctx, adguard.QueryLogParams{
			OlderThan:      prev[c.Name()].O,
			Limit:          pageSize,
			Search:         filter.Search,
			ResponseStatus: filter.Status,
		})
	})

	// Collect successful per-instance entries and record failures.
	byInstance := map[string][]adguard.QueryLogItem{}
	groups := make([]taggedEntries, 0, len(results))
	var errs []InstanceError
	failed := map[string]bool{}
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, InstanceError{Instance: r.Instance, Err: r.Err})
			failed[r.Instance] = true
			continue
		}
		byInstance[r.Instance] = r.Value.Data
		groups = append(groups, taggedEntries{Instance: r.Instance, Entries: r.Value.Data})
	}

	merged := mergeDescending(groups, pageSize)

	next := buildNextCursor(prev, filter, clients, byInstance, failed, merged.Consumed, pageSize)
	hasMore := anyNotDone(next, filter, clients)

	page := LogsPage{
		Entries: merged.Page,
		HasMore: hasMore,
		Errors:  errs,
	}
	if hasMore {
		page.NextCursor = Cursor{V: cursorVersion, I: next}.Encode()
	}
	return page
}

// buildNextCursor computes the per-instance cursor state for the next page.
func buildNextCursor(
	prev map[string]InstanceCursor,
	filter Filter,
	clients []*adguard.Client,
	byInstance map[string][]adguard.QueryLogItem,
	failed map[string]bool,
	consumed map[string]int,
	pageSize int,
) map[string]InstanceCursor {
	next := map[string]InstanceCursor{}
	for _, c := range clients {
		name := c.Name()
		if !filter.selected(name) {
			continue
		}
		// Carry forward instances already marked done.
		if prev[name].D {
			next[name] = InstanceCursor{O: prev[name].O, D: true}
			continue
		}
		// A failed fetch keeps its previous cursor unchanged so the same window
		// is retried on the next load-more; it is never marked done.
		if failed[name] {
			next[name] = prev[name]
			continue
		}

		returned := byInstance[name]
		c := consumed[name]
		newO := prev[name].O
		if c > 0 {
			newO = returned[c-1].RawTime // oldest consumed entry (prefix of newest-first list)
		}
		done := len(returned) < pageSize && c == len(returned)
		next[name] = InstanceCursor{O: newO, D: done}
	}
	return next
}

// anyNotDone reports whether any selected instance still has data to serve.
func anyNotDone(next map[string]InstanceCursor, filter Filter, clients []*adguard.Client) bool {
	for _, c := range clients {
		if !filter.selected(c.Name()) {
			continue
		}
		if !next[c.Name()].D {
			return true
		}
	}
	return false
}
