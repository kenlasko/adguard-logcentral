// Command server runs the AdGuard log aggregator web application.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/aggregate"
	"github.com/kenlasko/adguard-log-aggregator/internal/auth"
	"github.com/kenlasko/adguard-log-aggregator/internal/buildinfo"
	"github.com/kenlasko/adguard-log-aggregator/internal/config"
	"github.com/kenlasko/adguard-log-aggregator/internal/web"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		bootstrap := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		for _, problem := range unwrapJoined(err) {
			bootstrap.Error("configuration error", "problem", problem)
		}
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	build := buildinfo.Get()
	logger.Info("starting adguard log central",
		"version", build.Version, "commit", build.Commit, "build_date", build.Date)

	if err := run(cfg, logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// run wires dependencies and blocks until a shutdown signal is received.
func run(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	codec, err := auth.NewCodec(cfg.SessionSecret, cfg.CookieSecure)
	if err != nil {
		return err
	}

	// OIDC discovery is a network call; give it a bounded startup budget.
	discoveryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	authn, err := auth.NewAuthenticator(discoveryCtx, cfg, codec, logger)
	if err != nil {
		return err
	}

	clients := aggregate.NewClients(cfg.Instances, cfg.AdGuardTimeout)
	srv, err := web.NewServer(cfg, clients, authn, auth.NewMiddleware(codec), logger)
	if err != nil {
		return err
	}

	httpServer := srv.HTTPServer()
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", httpServer.Addr, "instances", len(cfg.Instances))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}

// unwrapJoined splits an errors.Join aggregate into its individual messages so
// each configuration problem is logged on its own line.
func unwrapJoined(err error) []string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := make([]string, 0, len(joined.Unwrap()))
		for _, e := range joined.Unwrap() {
			parts = append(parts, e.Error())
		}
		return parts
	}
	return []string{err.Error()}
}
