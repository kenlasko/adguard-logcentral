package adguard

import "context"

// Status fetches the running/version/protection state of this instance. It is
// used as a lightweight health probe and does no query log fan-out.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	if err := c.get(ctx, "/status", nil, &st); err != nil {
		return Status{}, err
	}
	return st, nil
}
