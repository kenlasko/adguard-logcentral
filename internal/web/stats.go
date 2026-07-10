package web

import (
	"context"
	"net/http"

	"github.com/kenlasko/adguard-logcentral/internal/aggregate"
	"github.com/kenlasko/adguard-logcentral/internal/auth"
)

// statsView is the data model shared by the stats page and its panels fragment.
type statsView struct {
	Session auth.Session
	Stats   aggregate.MergedStats
}

// handleStatsPage renders the full stats page.
func (s *Server) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "stats", s.buildStatsView(r))
}

// handleStatsPartial renders the stats panels fragment (polled every 60s).
func (s *Server) handleStatsPartial(w http.ResponseWriter, r *http.Request) {
	s.renderFragment(w, "stats_panels", s.buildStatsView(r))
}

func (s *Server) buildStatsView(r *http.Request) statsView {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.AdGuardTimeout)
	defer cancel()
	stats := aggregate.FetchStats(ctx, s.clients)
	sess, _ := auth.SessionFrom(r.Context())
	return statsView{Session: sess, Stats: stats}
}
