package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// validEnv returns a minimal environment that passes all validation, which
// individual tests then mutate to exercise specific failures.
func validEnv() map[string]string {
	return map[string]string{
		"ADGUARD_1_URL":      "http://10.0.0.2:3000",
		"ADGUARD_1_USERNAME": "admin",
		"ADGUARD_1_PASSWORD": "secret",
		"OIDC_ISSUER_URL":    "https://id.example.com",
		"OIDC_CLIENT_ID":     "client",
		"OIDC_CLIENT_SECRET": "clientsecret",
		"OIDC_REDIRECT_URL":  "https://app.example.com/auth/callback",
		"SESSION_SECRET":     strings.Repeat("x", 32),
	}
}

func TestFromEnvDefaults(t *testing.T) {
	cfg, err := fromEnv(mapGetenv(validEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AdGuardTimeout != 5*time.Second {
		t.Errorf("AdGuardTimeout default = %v, want 5s", cfg.AdGuardTimeout)
	}
	if cfg.SessionDuration != 12*time.Hour {
		t.Errorf("SessionDuration default = %v, want 12h", cfg.SessionDuration)
	}
	if !cfg.CookieSecure {
		t.Error("CookieSecure default should be true")
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr default = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.PageSize != 50 {
		t.Errorf("PageSize default = %d, want 50", cfg.PageSize)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel default = %v, want info", cfg.LogLevel)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	env := validEnv()
	env["ADGUARD_TIMEOUT"] = "10s"
	env["SESSION_DURATION"] = "1h"
	env["COOKIE_SECURE"] = "false"
	env["LISTEN_ADDR"] = ":9090"
	env["PAGE_SIZE"] = "25"
	env["LOG_LEVEL"] = "debug"

	cfg, err := fromEnv(mapGetenv(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AdGuardTimeout != 10*time.Second {
		t.Errorf("AdGuardTimeout = %v", cfg.AdGuardTimeout)
	}
	if cfg.SessionDuration != time.Hour {
		t.Errorf("SessionDuration = %v", cfg.SessionDuration)
	}
	if cfg.CookieSecure {
		t.Error("CookieSecure should be false")
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.PageSize != 25 {
		t.Errorf("PageSize = %d", cfg.PageSize)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
}

func TestFromEnvMissingOIDC(t *testing.T) {
	env := validEnv()
	delete(env, "OIDC_ISSUER_URL")
	delete(env, "OIDC_CLIENT_ID")

	_, err := fromEnv(mapGetenv(env))
	if err == nil {
		t.Fatal("expected error for missing OIDC vars")
	}
	msg := err.Error()
	if !strings.Contains(msg, "OIDC_ISSUER_URL") || !strings.Contains(msg, "OIDC_CLIENT_ID") {
		t.Errorf("error should list both missing OIDC vars: %s", msg)
	}
}

func TestFromEnvShortSessionSecret(t *testing.T) {
	env := validEnv()
	env["SESSION_SECRET"] = "tooshort"
	_, err := fromEnv(mapGetenv(env))
	if err == nil || !strings.Contains(err.Error(), "SESSION_SECRET must be at least 32") {
		t.Errorf("expected session secret length error, got %v", err)
	}
}

func TestFromEnvBadValuesAggregated(t *testing.T) {
	env := validEnv()
	env["ADGUARD_TIMEOUT"] = "not-a-duration"
	env["PAGE_SIZE"] = "9000"
	env["LOG_LEVEL"] = "chatty"
	env["COOKIE_SECURE"] = "maybe"

	_, err := fromEnv(mapGetenv(env))
	if err == nil {
		t.Fatal("expected aggregated errors")
	}
	msg := err.Error()
	for _, want := range []string{"ADGUARD_TIMEOUT", "PAGE_SIZE", "LOG_LEVEL", "COOKIE_SECURE"} {
		if !strings.Contains(msg, want) {
			t.Errorf("aggregated error missing %s: %s", want, msg)
		}
	}
}

func TestFromEnvPageSizeBounds(t *testing.T) {
	for _, ps := range []string{"0", "-1", "1001"} {
		env := validEnv()
		env["PAGE_SIZE"] = ps
		if _, err := fromEnv(mapGetenv(env)); err == nil {
			t.Errorf("PAGE_SIZE=%s should be rejected", ps)
		}
	}
}
