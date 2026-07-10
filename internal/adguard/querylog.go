package adguard

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// QueryLogParams are the supported filters for a query log request. Zero-valued
// fields are omitted from the outbound request so AdGuard applies its defaults.
type QueryLogParams struct {
	// OlderThan is the raw "time" cursor of the last item already seen; the
	// API returns entries strictly older than it. Empty means "from newest".
	OlderThan string
	// Limit caps the number of returned entries.
	Limit int
	// Search matches a domain or client (substring).
	Search string
	// ResponseStatus is one of the AdGuard response_status filter values
	// (all, filtered, blocked, whitelisted, rewritten, ...).
	ResponseStatus string
}

// QueryLog fetches query log entries from this instance. Each returned item has
// its ParsedTime derived from RawTime (RFC3339Nano) and its Instance tagged, so
// the aggregator can merge and sort across instances without re-parsing.
func (c *Client) QueryLog(ctx context.Context, p QueryLogParams) (QueryLogResponse, error) {
	q := url.Values{}
	if p.OlderThan != "" {
		q.Set("older_than", p.OlderThan)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Search != "" {
		q.Set("search", p.Search)
	}
	if p.ResponseStatus != "" {
		q.Set("response_status", p.ResponseStatus)
	}

	var resp QueryLogResponse
	if err := c.get(ctx, "/querylog", q, &resp); err != nil {
		return QueryLogResponse{}, err
	}

	// Build a new slice rather than mutating the decoded items in place.
	enriched := make([]QueryLogItem, len(resp.Data))
	for i, item := range resp.Data {
		item.Instance = c.name
		if t, err := time.Parse(time.RFC3339Nano, item.RawTime); err == nil {
			item.ParsedTime = t
		}
		enriched[i] = item
	}
	return QueryLogResponse{Oldest: resp.Oldest, Data: enriched}, nil
}
