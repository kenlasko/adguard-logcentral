package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// maxInstances bounds the indexed environment scan so a misconfiguration
// cannot loop unbounded. The design targets a handful of instances.
const maxInstances = 64

// Instance is an immutable description of a single AdGuard Home endpoint.
type Instance struct {
	Name     string
	URL      string
	Username string
	Password string
}

// loadInstances scans ADGUARD_<n>_URL/_USERNAME/_PASSWORD/_NAME for
// n = 1..maxInstances, stopping at the first index whose URL is unset.
// It returns every instance found together with a slice of all problems
// discovered (never fail-fast), so callers can surface them at once.
func loadInstances(getenv func(string) string) ([]Instance, []error) {
	var instances []Instance
	var errs []error

	seenNames := map[string]int{}
	runEnded := false

	for n := 1; n <= maxInstances; n++ {
		rawURL := strings.TrimSpace(getenv(fmt.Sprintf("ADGUARD_%d_URL", n)))
		username := getenv(fmt.Sprintf("ADGUARD_%d_USERNAME", n))
		password := getenv(fmt.Sprintf("ADGUARD_%d_PASSWORD", n))
		name := strings.TrimSpace(getenv(fmt.Sprintf("ADGUARD_%d_NAME", n)))

		if rawURL == "" {
			// The contiguous run of instances ends at the first unset URL.
			runEnded = true
			// Orphan credential/name data at an empty index is almost always a typo.
			if username != "" || password != "" || name != "" {
				errs = append(errs, fmt.Errorf("ADGUARD_%d_* is set but ADGUARD_%d_URL is empty (index gap; indices must be contiguous from 1)", n, n))
			}
			continue
		}

		// A URL that appears after the run has ended is a gap (e.g. 1 and 3 set, 2 unset).
		if runEnded {
			errs = append(errs, fmt.Errorf("ADGUARD_%d_URL is set after an index gap; instance indices must be contiguous starting at 1", n))
		}

		if username == "" {
			errs = append(errs, fmt.Errorf("ADGUARD_%d_USERNAME is required when ADGUARD_%d_URL is set", n, n))
		}
		if password == "" {
			errs = append(errs, fmt.Errorf("ADGUARD_%d_PASSWORD is required when ADGUARD_%d_URL is set", n, n))
		}

		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			errs = append(errs, fmt.Errorf("ADGUARD_%d_URL is not a valid absolute URL: %q", n, rawURL))
			continue
		}

		if name == "" {
			name = parsed.Host
		}
		if prev, dup := seenNames[name]; dup {
			errs = append(errs, fmt.Errorf("duplicate instance name %q (indices %d and %d); set a unique ADGUARD_<n>_NAME", name, prev, n))
		} else {
			seenNames[name] = n
		}

		instances = append(instances, Instance{
			Name:     name,
			URL:      strings.TrimRight(rawURL, "/"),
			Username: username,
			Password: password,
		})
	}

	if len(instances) == 0 && len(errs) == 0 {
		errs = append(errs, fmt.Errorf("no AdGuard instances configured; set ADGUARD_1_URL, ADGUARD_1_USERNAME, ADGUARD_1_PASSWORD"))
	}

	return instances, errs
}

// osGetenv adapts os.Getenv to the getenv function signature used above.
func osGetenv(key string) string { return os.Getenv(key) }
