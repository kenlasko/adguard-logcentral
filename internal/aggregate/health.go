package aggregate

import (
	"context"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
)

// healthTimeout bounds the health probe so one slow instance cannot delay the
// whole health bar. It is deliberately shorter than the query log timeout.
const healthTimeout = 2 * time.Second

// InstanceHealth is the immutable health state of one instance.
type InstanceHealth struct {
	Name              string
	Reachable         bool
	Version           string
	ProtectionEnabled bool
	Err               string
}

// FetchHealth probes /control/status on every instance with a short timeout and
// returns one InstanceHealth per instance, in stable order.
func FetchHealth(ctx context.Context, clients []*adguard.Client) []InstanceHealth {
	probeCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	results := fanOut(probeCtx, clients, func(ctx context.Context, c *adguard.Client) (adguard.Status, error) {
		return c.Status(ctx)
	})

	health := make([]InstanceHealth, len(results))
	for i, r := range results {
		if r.Err != nil {
			health[i] = InstanceHealth{Name: r.Instance, Reachable: false, Err: r.Err.Error()}
			continue
		}
		health[i] = InstanceHealth{
			Name:              r.Instance,
			Reachable:         r.Value.Running,
			Version:           r.Value.Version,
			ProtectionEnabled: r.Value.ProtectionEnabled,
		}
	}
	return health
}
