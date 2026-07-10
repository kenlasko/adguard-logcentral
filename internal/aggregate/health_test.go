package aggregate

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

func TestFetchHealthReachableAndUnreachable(t *testing.T) {
	up := adguardtest.New("dns1", "u", "p", nil).
		WithStatus(adguardtest.StatusPayload{Running: true, Version: "v1.2.3", ProtectionEnabled: true})
	s1 := up.Server()
	defer s1.Close()

	down := adguardtest.New("dns2", "u", "p", nil).WithFailures(adguardtest.Failures{Status500: true})
	s2 := down.Server()
	defer s2.Close()

	clients := []*adguard.Client{
		adguard.New(config.Instance{Name: "dns1", URL: s1.URL, Username: "u", Password: "p"}, &http.Client{Timeout: time.Second}),
		adguard.New(config.Instance{Name: "dns2", URL: s2.URL, Username: "u", Password: "p"}, &http.Client{Timeout: time.Second}),
	}

	health := FetchHealth(context.Background(), clients)
	if len(health) != 2 {
		t.Fatalf("expected 2 health entries, got %d", len(health))
	}
	// Order is stable (matches client order).
	if health[0].Name != "dns1" || !health[0].Reachable || health[0].Version != "v1.2.3" || !health[0].ProtectionEnabled {
		t.Errorf("dns1 health wrong: %+v", health[0])
	}
	if health[1].Name != "dns2" || health[1].Reachable || health[1].Err == "" {
		t.Errorf("dns2 should be unreachable with an error: %+v", health[1])
	}
}
