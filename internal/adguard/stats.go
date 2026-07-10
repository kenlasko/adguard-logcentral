package adguard

import "context"

// Stats fetches aggregate counters and top-N lists from this instance.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	if err := c.get(ctx, "/stats", nil, &s); err != nil {
		return Stats{}, err
	}
	return s, nil
}
