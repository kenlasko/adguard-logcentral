package aggregate

import (
	"testing"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
)

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		// Exact matching is the default: no wildcard means full-string equality.
		{"exact match", "192.168.1.2", "192.168.1.2", true},
		{"exact rejects longer", "192.168.1.2", "192.168.1.20", false},
		{"exact rejects longer 21", "192.168.1.2", "192.168.1.21", false},
		{"exact rejects prefix of pattern", "192.168.1.2", "192.168.1.", false},
		{"exact rejects substring container", "example.com", "www.example.com", false},

		// Trailing wildcard: prefix match.
		{"trailing star matches base", "192.168.1.2*", "192.168.1.2", true},
		{"trailing star matches longer", "192.168.1.2*", "192.168.1.20", true},
		{"trailing star matches longer 21", "192.168.1.2*", "192.168.1.21", true},
		{"trailing star rejects different prefix", "192.168.1.2*", "192.168.1.3", false},

		// Leading wildcard: suffix match.
		{"leading star matches suffix", "*.example.com", "www.example.com", true},
		{"leading star matches exact suffix", "*.example.com", "a.example.com", true},
		{"leading star rejects other suffix", "*.example.com", "www.example.net", false},

		// Surrounding wildcards: substring match.
		{"both stars substring", "*google*", "www.google.com", true},
		{"both stars no match", "*google*", "www.example.com", false},

		// Interior wildcard.
		{"interior star", "192.168.*.2", "192.168.5.2", true},
		{"interior star empty", "192.168.*.2", "192.168..2", true},
		{"interior star no suffix", "192.168.*.2", "192.168.5.20", false},

		// Case-insensitivity.
		{"case insensitive exact", "Example.COM", "example.com", true},
		{"case insensitive wildcard", "*EXAMPLE*", "www.example.com", true},

		// Bare wildcard matches anything.
		{"bare star", "*", "anything.at.all", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchGlob(tc.pattern, tc.value); got != tc.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
			}
		})
	}
}

func TestAdguardSearchTerm(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{"192.168.1.2", "192.168.1.2"}, // no wildcard: whole pattern
		{"192.168.1.2*", "192.168.1.2"},
		{"*.example.com", ".example.com"},
		{"*google*", "google"},
		{"192.168.*.2", "192.168."}, // longest literal segment
		{"*", ""},                   // no literal at all
		{"", ""},
	}
	for _, tc := range cases {
		if got := adguardSearchTerm(tc.pattern); got != tc.want {
			t.Errorf("adguardSearchTerm(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
}

func TestMatchesSearchDomainOrClient(t *testing.T) {
	item := adguard.QueryLogItem{
		Client:   "192.168.1.2",
		Question: adguard.Question{Name: "ads.example.com"},
	}

	cases := []struct {
		pattern string
		want    bool
	}{
		{"", true},                 // empty matches everything
		{"192.168.1.2", true},      // exact client
		{"192.168.1.20", false},    // client near-miss excluded
		{"ads.example.com", true},  // exact domain
		{"example.com", false},     // domain substring excluded without wildcard
		{"*.example.com", true},    // domain suffix wildcard
		{"192.168.1.2*", true},     // client prefix wildcard
		{"nomatch.invalid", false}, // neither
	}
	for _, tc := range cases {
		f := Filter{Search: tc.pattern}
		if got := f.matchesSearch(item); got != tc.want {
			t.Errorf("matchesSearch(%q) = %v, want %v", tc.pattern, got, tc.want)
		}
	}
}

func TestFilterEntriesImmutable(t *testing.T) {
	in := []adguard.QueryLogItem{
		{Client: "192.168.1.2", Question: adguard.Question{Name: "a.com"}},
		{Client: "192.168.1.20", Question: adguard.Question{Name: "b.com"}},
	}
	f := Filter{Search: "192.168.1.2"}
	out := f.filterEntries(in)

	if len(out) != 1 || out[0].Client != "192.168.1.2" {
		t.Fatalf("expected only the exact client, got %+v", out)
	}
	if len(in) != 2 {
		t.Errorf("input slice was mutated: %+v", in)
	}

	// Empty search returns the input untouched.
	if got := (Filter{}).filterEntries(in); len(got) != 2 {
		t.Errorf("empty search should return all entries, got %d", len(got))
	}
}
