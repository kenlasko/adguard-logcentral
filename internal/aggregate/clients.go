package aggregate

import (
	"net/http"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

// NewClients builds one adguard.Client per configured instance, all sharing a
// single *http.Client whose timeout bounds every outbound call. Instance order
// is preserved so the UI shows a stable ordering.
func NewClients(instances []config.Instance, timeout time.Duration) []*adguard.Client {
	shared := &http.Client{Timeout: timeout}
	clients := make([]*adguard.Client, len(instances))
	for i, inst := range instances {
		clients[i] = adguard.New(inst, shared)
	}
	return clients
}
