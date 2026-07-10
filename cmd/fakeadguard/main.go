// Command fakeadguard serves the adguardtest fixture as a standalone process
// so the aggregator can be exercised end-to-end without real AdGuard Home
// instances. It generates a few hundred deterministic query log entries.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard/adguardtest"
)

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	name := flag.String("name", "dns1", "instance display name (also seeds data)")
	user := flag.String("user", "admin", "basic auth username")
	pass := flag.String("pass", "password", "basic auth password")
	count := flag.Int("count", 500, "number of generated query log entries")
	flag.Parse()

	// Seed varies by name so two instances serve interleaved-but-distinct data.
	seed := 0
	for _, r := range *name {
		seed += int(r)
	}
	entries := adguardtest.Generate(*count, time.Now(), 250*time.Millisecond, seed)
	fake := adguardtest.New(*name, *user, *pass, entries)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           fake.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("fakeadguard %q listening on %s (%d entries, user=%s)", *name, *addr, *count, *user)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
