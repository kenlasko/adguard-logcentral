package aggregate

import (
	"context"
	"sync"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
)

// Result is the outcome of one instance's fan-out call. Exactly one of Value's
// usefulness and Err is meaningful: on error Value is the zero value.
type Result[T any] struct {
	Instance string
	Value    T
	Err      error
}

// fanOut runs fn concurrently against every client, one goroutine each, sharing
// ctx (typically carrying a timeout). Results are returned in the same order as
// clients, so output is deterministic regardless of completion order.
func fanOut[T any](ctx context.Context, clients []*adguard.Client, fn func(context.Context, *adguard.Client) (T, error)) []Result[T] {
	results := make([]Result[T], len(clients))
	var wg sync.WaitGroup
	wg.Add(len(clients))
	for i, c := range clients {
		go func(i int, c *adguard.Client) {
			defer wg.Done()
			v, err := fn(ctx, c)
			results[i] = Result[T]{Instance: c.Name(), Value: v, Err: err}
		}(i, c)
	}
	wg.Wait()
	return results
}
