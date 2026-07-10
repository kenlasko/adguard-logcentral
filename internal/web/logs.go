package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/kenlasko/adguard-log-aggregator/internal/aggregate"
	"github.com/kenlasko/adguard-log-aggregator/internal/auth"
)

// instanceOption is one instance checkbox in the filter form.
type instanceOption struct {
	Name    string
	Checked bool
}

// logsView is the data model shared by the logs page and its rows fragment.
type logsView struct {
	Session     auth.Session
	Instances   []instanceOption
	Search      string
	Status      string
	AutoRefresh bool
	Page        aggregate.LogsPage
}

// handleLogsPage renders the full logs page (page 1, no cursor).
func (s *Server) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	// Root only; anything else under "/" is a 404, not the logs page.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	view := s.buildLogsView(r, nil)
	s.renderPage(w, "logs", view)
}

// handleLogsPartial renders the rows fragment for filter changes (no cursor,
// replaces the table body) and load-more (cursor present, appends).
func (s *Server) handleLogsPartial(w http.ResponseWriter, r *http.Request) {
	cursor := aggregate.DecodeCursor(r.URL.Query().Get("cursor"))
	view := s.buildLogsView(r, cursor)
	s.renderFragment(w, "logs_rows", view)
}

// buildLogsView parses filters, fetches one page, and assembles the view model.
func (s *Server) buildLogsView(r *http.Request, cursor *aggregate.Cursor) logsView {
	filter := parseFilter(r)

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.AdGuardTimeout)
	defer cancel()
	page := aggregate.FetchLogs(ctx, s.clients, filter, cursor, s.cfg.PageSize)

	sess, _ := auth.SessionFrom(r.Context())
	return logsView{
		Session:     sess,
		Instances:   s.instanceOptions(filter.Instances),
		Search:      filter.Search,
		Status:      filter.Status,
		AutoRefresh: r.URL.Query().Get("auto") == "on",
		Page:        page,
	}
}

// parseFilter builds an aggregate.Filter from the request query. An empty
// instance selection means "all instances" (aggregate treats nil as all).
func parseFilter(r *http.Request) aggregate.Filter {
	q := r.URL.Query()
	return aggregate.Filter{
		Search:    strings.TrimSpace(q.Get("search")),
		Status:    q.Get("status"),
		Instances: q["instance"],
	}
}

// instanceOptions computes checkbox state: with no explicit selection every
// instance is checked; otherwise only the selected ones are.
func (s *Server) instanceOptions(selected []string) []instanceOption {
	all := len(selected) == 0
	set := make(map[string]bool, len(selected))
	for _, n := range selected {
		set[n] = true
	}
	opts := make([]instanceOption, len(s.instances))
	for i, n := range s.instances {
		opts[i] = instanceOption{Name: n, Checked: all || set[n]}
	}
	return opts
}
