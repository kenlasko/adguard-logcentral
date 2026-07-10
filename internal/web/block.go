package web

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
)

// domainPattern validates a domain before it is woven into an AdGuard rule.
// Restricting to hostname characters prevents rule injection: a value with
// spaces, newlines, or rule syntax ("|", "^", "@") is rejected rather than
// changing the semantics of the generated rule. Labels may lead with an
// underscore ("_dmarc", "_acme-challenge") as those appear in real query logs.
// Go's regexp (RE2) runs in guaranteed linear time, so the pattern is ReDoS-safe.
var domainPattern = regexp.MustCompile(`^[a-zA-Z0-9_]([a-zA-Z0-9_-]*[a-zA-Z0-9_])?(\.[a-zA-Z0-9_]([a-zA-Z0-9_-]*[a-zA-Z0-9_])?)*$`)

// blockCellView drives the per-row Block/Unblock control fragment.
type blockCellView struct {
	Domain  string
	Blocked bool
}

// handleBlock applies or removes a block rule for one domain on the instance
// the operator selected. It is a POST endpoint; SameSite=Lax on the session
// cookie means a cross-site POST arrives without the cookie and is rejected by
// the auth middleware, so no separate CSRF token is needed. On success it
// returns the refreshed control cell so htmx can swap it in place.
func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	instance := r.PostForm.Get("instance")
	domain := strings.TrimSuffix(strings.TrimSpace(r.PostForm.Get("domain")), ".")
	block := r.PostForm.Get("action") == "block"

	client := s.clientByName(instance)
	if client == nil {
		http.Error(w, "unknown instance", http.StatusBadRequest)
		return
	}
	if !validDomain(domain) {
		http.Error(w, "invalid domain", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.AdGuardTimeout)
	defer cancel()
	if err := client.SetDomainBlock(ctx, domain, block); err != nil {
		s.logger.Error("set domain block failed", "instance", instance, "domain", domain, "block", block, "error", err)
		http.Error(w, "failed to update rule", http.StatusBadGateway)
		return
	}

	// The cell now reflects the new state: after blocking it offers Unblock.
	s.renderFragment(w, "block_cell", blockCellView{Domain: domain, Blocked: block})
}

// clientByName returns the client for a configured instance name, or nil when
// no such instance exists.
func (s *Server) clientByName(name string) *adguard.Client {
	for _, c := range s.clients {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// validDomain reports whether a domain is a syntactically plausible hostname
// safe to embed in a filtering rule.
func validDomain(domain string) bool {
	return domain != "" && len(domain) <= 253 && domainPattern.MatchString(domain)
}
