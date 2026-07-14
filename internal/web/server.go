// Package web is the HTTP layer: routing, template rendering, and the log,
// health, and stats handlers. It depends on the aggregate package for data and
// the auth package for session enforcement.
package web

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/auth"
	"github.com/kenlasko/adguard-logcentral/internal/buildinfo"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

// Server holds everything the HTTP handlers need. It is constructed once at
// startup and is safe for concurrent use.
type Server struct {
	cfg       config.Config
	clients   []*adguard.Client
	instances []string
	templates *templates
	auth      *auth.Authenticator
	mw        *auth.Middleware
	logger    *slog.Logger
}

// NewServer builds the web server. The authenticator is injected so main can
// perform OIDC discovery (a network call) with its own context and error
// handling before wiring the server.
func NewServer(cfg config.Config, clients []*adguard.Client, authn *auth.Authenticator, mw *auth.Middleware, logger *slog.Logger) (*Server, error) {
	names := make([]string, len(cfg.Instances))
	for i, inst := range cfg.Instances {
		names[i] = inst.Name
	}
	tmpls, err := newTemplates(names)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:       cfg,
		clients:   clients,
		instances: names,
		templates: tmpls,
		auth:      authn,
		mw:        mw,
		logger:    logger,
	}, nil
}

// Handler builds the routed, middleware-wrapped http.Handler for the whole app.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Application routes.
	mux.HandleFunc("GET /", s.handleLogsPage)
	mux.HandleFunc("GET /partials/logs", s.handleLogsPartial)
	mux.HandleFunc("POST /partials/block", s.handleBlock)
	mux.HandleFunc("GET /partials/health", s.handleHealthPartial)
	mux.HandleFunc("GET /stats", s.handleStatsPage)
	mux.HandleFunc("GET /partials/stats", s.handleStatsPartial)
	mux.HandleFunc("GET /healthz", healthz)

	// Static assets embedded in the binary.
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServer(http.FS(staticSub)))))

	// Auth endpoints (allowlisted by the middleware).
	s.auth.Routes(mux)

	return securityHeaders(s.mw.RequireAuth(mux))
}

// securityHeaders sets defense-in-depth response headers on every response. The
// CSP is deliberately strict: the app self-hosts its only script (htmx) and
// stylesheet, so 'self' needs no inline exceptions. frame-ancestors 'none'
// blocks clickjacking of the block/unblock controls; nosniff blocks MIME
// confusion; no-referrer keeps query-log URLs out of upstream Referer headers.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self'; " +
		"img-src 'self' data:; object-src 'none'; base-uri 'none'; " +
		"form-action 'self'; frame-ancestors 'none'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// HTTPServer wraps Handler in an *http.Server with sane timeouts.
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.Handler(),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// cacheStatic adds a modest cache header to embedded static assets.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}

// healthzResponse is the JSON body of the liveness probe. It also reports the
// build metadata so a running container can be curled to confirm which release
// is deployed.
type healthzResponse struct {
	Status string         `json:"status"`
	Build  buildinfo.Info `json:"build"`
}

// healthz is an unauthenticated liveness probe with no upstream fan-out.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthzResponse{Status: "ok", Build: buildinfo.Get()})
}
