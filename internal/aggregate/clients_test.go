package aggregate

import (
	"testing"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

func TestNewClientsPreservesOrderAndNames(t *testing.T) {
	instances := []config.Instance{
		{Name: "dns1", URL: "http://a:3000", Username: "u", Password: "p"},
		{Name: "dns2", URL: "http://b:3000", Username: "u", Password: "p"},
		{Name: "dns3", URL: "http://c:3000", Username: "u", Password: "p"},
	}
	clients := NewClients(instances, 5*time.Second)
	if len(clients) != 3 {
		t.Fatalf("expected 3 clients, got %d", len(clients))
	}
	for i, want := range []string{"dns1", "dns2", "dns3"} {
		if clients[i].Name() != want {
			t.Errorf("client %d name = %q, want %q", i, clients[i].Name(), want)
		}
	}
}

func TestNewClientsEmpty(t *testing.T) {
	if got := NewClients(nil, time.Second); len(got) != 0 {
		t.Errorf("expected no clients, got %d", len(got))
	}
}
