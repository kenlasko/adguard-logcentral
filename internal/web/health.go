package web

import (
	"context"
	"net/http"

	"github.com/kenlasko/adguard-logcentral/internal/aggregate"
)

// healthView is the data model for the health bar fragment.
type healthView struct {
	Instances []aggregate.InstanceHealth
}

// handleHealthPartial fans out status probes and renders the health bar, which
// the layout polls every 15 seconds.
func (s *Server) handleHealthPartial(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.AdGuardTimeout)
	defer cancel()
	health := aggregate.FetchHealth(ctx, s.clients)
	s.renderFragment(w, "health_bar", healthView{Instances: health})
}
