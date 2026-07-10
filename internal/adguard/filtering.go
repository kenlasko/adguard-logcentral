package adguard

import (
	"context"
	"strings"
)

// FilteringStatus mirrors the subset of /control/filtering/status this
// aggregator needs: the operator's custom rule list. The full endpoint also
// returns configured filter subscriptions, which are not used here.
type FilteringStatus struct {
	UserRules []string `json:"user_rules"`
}

// FilteringStatus fetches the current custom filtering rules for this instance.
func (c *Client) FilteringStatus(ctx context.Context) (FilteringStatus, error) {
	var fs FilteringStatus
	if err := c.get(ctx, "/filtering/status", nil, &fs); err != nil {
		return FilteringStatus{}, err
	}
	return fs, nil
}

// SetUserRules replaces the entire custom rules list for this instance via
// /control/filtering/set_rules, matching how AdGuard Home's own UI persists
// user rules.
func (c *Client) SetUserRules(ctx context.Context, rules []string) error {
	return c.post(ctx, "/filtering/set_rules", map[string]any{"rules": rules})
}

// SetDomainBlock blocks or unblocks a single domain by editing this instance's
// custom rules. Blocking adds "||domain^" and drops any matching "@@||domain^"
// allow rule; unblocking adds "@@||domain^" and drops any matching "||domain^"
// block rule. With adguardhome-sync in place the change propagates to the other
// instances, so the operator only needs to target one.
func (c *Client) SetDomainBlock(ctx context.Context, domain string, block bool) error {
	fs, err := c.FilteringStatus(ctx)
	if err != nil {
		return err
	}
	return c.SetUserRules(ctx, mergeDomainRule(fs.UserRules, domain, block))
}

// blockRule and allowRule return the canonical AdGuard rule text for a domain.
func blockRule(domain string) string { return "||" + domain + "^" }
func allowRule(domain string) string { return "@@||" + domain + "^" }

// mergeDomainRule returns a new rules slice expressing the desired block state
// for exactly one domain. It removes both the same-kind duplicate and the
// opposing canonical rule for that domain, then appends the desired rule. Every
// unrelated rule is preserved in order, and the input slice is never mutated,
// so the operation is idempotent and leaves hand-written rules untouched.
func mergeDomainRule(current []string, domain string, block bool) []string {
	want := allowRule(domain)
	opposing := blockRule(domain)
	if block {
		want, opposing = blockRule(domain), allowRule(domain)
	}

	out := make([]string, 0, len(current)+1)
	for _, r := range current {
		switch strings.TrimSpace(r) {
		case want, opposing:
			continue // drop existing same-kind or opposing rule for this domain
		default:
			out = append(out, r)
		}
	}
	return append(out, want)
}
