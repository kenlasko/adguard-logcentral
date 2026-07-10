package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

func TestHealthPartialRendersReachableAndUnreachable(t *testing.T) {
	up := adguardtest.New("dns1", "u", "p", nil).
		WithStatus(adguardtest.StatusPayload{Running: true, Version: "v9.9.9", ProtectionEnabled: true}).Server()
	defer up.Close()
	down := adguardtest.New("dns2", "u", "p", nil).WithFailures(adguardtest.Failures{Status500: true}).Server()
	defer down.Close()

	clients := []*adguard.Client{
		adguard.New(config.Instance{Name: "dns1", URL: up.URL, Username: "u", Password: "p"}, &http.Client{Timeout: 2 * time.Second}),
		adguard.New(config.Instance{Name: "dns2", URL: down.URL, Username: "u", Password: "p"}, &http.Client{Timeout: 2 * time.Second}),
	}

	s, codec := testServer(t, []string{"dns1", "dns2"}, clients, 50)
	h, cookie := authed(t, codec, s.handleHealthPartial)

	req := httptest.NewRequest("GET", "/partials/health", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "pill up") || !strings.Contains(body, "v9.9.9") {
		t.Errorf("expected reachable dns1 pill with version: %s", body)
	}
	if !strings.Contains(body, "pill down") || !strings.Contains(body, "unreachable") {
		t.Errorf("expected unreachable dns2 pill: %s", body)
	}
}

func TestHealthPartialShowsProtectionOff(t *testing.T) {
	up := adguardtest.New("dns1", "u", "p", nil).
		WithStatus(adguardtest.StatusPayload{Running: true, Version: "v1", ProtectionEnabled: false}).Server()
	defer up.Close()
	clients := []*adguard.Client{
		adguard.New(config.Instance{Name: "dns1", URL: up.URL, Username: "u", Password: "p"}, &http.Client{Timeout: 2 * time.Second}),
	}
	s, codec := testServer(t, []string{"dns1"}, clients, 50)
	h, cookie := authed(t, codec, s.handleHealthPartial)

	req := httptest.NewRequest("GET", "/partials/health", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "protection off") {
		t.Errorf("expected protection-off note: %s", rec.Body.String())
	}
}
