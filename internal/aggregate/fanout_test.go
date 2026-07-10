package aggregate

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

func TestFanOutStableOrderAndErrors(t *testing.T) {
	good := adguardtest.New("dns1", "u", "p", nil).
		WithStatus(adguardtest.StatusPayload{Running: true, Version: "v1"}).Server()
	defer good.Close()
	bad := adguardtest.New("dns2", "u", "p", nil).WithFailures(adguardtest.Failures{Status500: true}).Server()
	defer bad.Close()

	clients := []*adguard.Client{
		adguard.New(config.Instance{Name: "dns1", URL: good.URL, Username: "u", Password: "p"}, &http.Client{Timeout: time.Second}),
		adguard.New(config.Instance{Name: "dns2", URL: bad.URL, Username: "u", Password: "p"}, &http.Client{Timeout: time.Second}),
	}

	results := fanOut(context.Background(), clients, func(ctx context.Context, c *adguard.Client) (adguard.Status, error) {
		return c.Status(ctx)
	})

	// Results preserve client order regardless of completion order.
	if len(results) != 2 || results[0].Instance != "dns1" || results[1].Instance != "dns2" {
		t.Fatalf("unexpected order/length: %+v", results)
	}
	if results[0].Err != nil || results[0].Value.Version != "v1" {
		t.Errorf("dns1 result wrong: %+v", results[0])
	}
	if results[1].Err == nil {
		t.Error("dns2 should have errored")
	}
}

func TestFanOutEmptyClients(t *testing.T) {
	results := fanOut(context.Background(), nil, func(ctx context.Context, c *adguard.Client) (int, error) {
		return 0, errors.New("should not be called")
	})
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}
