// Package config loads and validates all runtime configuration from the
// environment. Nothing is read from disk; every setting comes from an
// environment variable or secret, per the project requirements.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Config is the immutable, fully validated application configuration.
type Config struct {
	Instances []Instance

	AdGuardTimeout time.Duration

	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	SessionSecret   string
	SessionDuration time.Duration
	CookieSecure    bool

	ListenAddr string
	PageSize   int
	LogLevel   slog.Level
}

// FromEnv builds a Config from the process environment, returning a single
// error that aggregates every problem discovered so operators can fix them
// all at once rather than one restart at a time.
func FromEnv() (Config, error) {
	return fromEnv(osGetenv)
}

// fromEnv is the testable core of FromEnv, parameterized on the getenv lookup.
func fromEnv(getenv func(string) string) (Config, error) {
	var errs []error

	instances, instErrs := loadInstances(getenv)
	errs = append(errs, instErrs...)

	cfg := Config{
		Instances:        instances,
		AdGuardTimeout:   parseDuration(getenv, "ADGUARD_TIMEOUT", 5*time.Second, &errs),
		OIDCIssuerURL:    strings.TrimSpace(getenv("OIDC_ISSUER_URL")),
		OIDCClientID:     strings.TrimSpace(getenv("OIDC_CLIENT_ID")),
		OIDCClientSecret: getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  strings.TrimSpace(getenv("OIDC_REDIRECT_URL")),
		SessionSecret:    getenv("SESSION_SECRET"),
		SessionDuration:  parseDuration(getenv, "SESSION_DURATION", 12*time.Hour, &errs),
		CookieSecure:     parseBool(getenv, "COOKIE_SECURE", true, &errs),
		ListenAddr:       defaultString(getenv("LISTEN_ADDR"), ":8080"),
		PageSize:         parsePageSize(getenv, &errs),
		LogLevel:         parseLogLevel(getenv, &errs),
	}

	for name, val := range map[string]string{
		"OIDC_ISSUER_URL":    cfg.OIDCIssuerURL,
		"OIDC_CLIENT_ID":     cfg.OIDCClientID,
		"OIDC_CLIENT_SECRET": cfg.OIDCClientSecret,
		"OIDC_REDIRECT_URL":  cfg.OIDCRedirectURL,
	} {
		if val == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}

	if len(cfg.SessionSecret) < 32 {
		errs = append(errs, fmt.Errorf("SESSION_SECRET must be at least 32 characters (got %d)", len(cfg.SessionSecret)))
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func parseDuration(getenv func(string) string, key string, fallback time.Duration, errs *[]error) time.Duration {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s is not a valid duration: %q", key, raw))
		return fallback
	}
	if d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be positive: %q", key, raw))
		return fallback
	}
	return d
}

func parseBool(getenv func(string) string, key string, fallback bool, errs *[]error) bool {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s is not a valid boolean: %q", key, raw))
		return fallback
	}
	return b
}

func parsePageSize(getenv func(string) string, errs *[]error) int {
	raw := strings.TrimSpace(getenv("PAGE_SIZE"))
	if raw == "" {
		return 50
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("PAGE_SIZE is not a valid integer: %q", raw))
		return 50
	}
	if n < 1 || n > 1000 {
		*errs = append(*errs, fmt.Errorf("PAGE_SIZE must be between 1 and 1000 (got %d)", n))
		return 50
	}
	return n
}

func parseLogLevel(getenv func(string) string, errs *[]error) slog.Level {
	raw := strings.ToLower(strings.TrimSpace(getenv("LOG_LEVEL")))
	switch raw {
	case "", "info":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		*errs = append(*errs, fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, error (got %q)", raw))
		return slog.LevelInfo
	}
}
