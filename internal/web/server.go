// Package web is the HTTP layer: routing, template rendering, and the log,
// health, and stats handlers. It depends on the aggregate package for data and
// the auth package for session enforcement.
package web

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
	"github.com/kenlasko/adguard-log-aggregator/internal/auth"
	"github.com/kenlasko/adguard-log-aggregator/internal/config"
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

	return s.mw.RequireAuth(mux)
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

// healthz is an unauthenticated liveness probe with no upstream fan-out.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
