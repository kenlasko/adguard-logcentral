package adguard

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard/adguardtest"
	"github.com/kenlasko/adguard-logcentral/internal/config"
)

func TestMergeDomainRule(t *testing.T) {
	cases := []struct {
		name    string
		current []string
		domain  string
		block   bool
		want    []string
	}{
		{
			name:    "block adds block rule",
			current: nil,
			domain:  "ads.example.com",
			block:   true,
			want:    []string{"||ads.example.com^"},
		},
		{
			name:    "unblock adds allow rule",
			current: nil,
			domain:  "good.example.com",
			block:   false,
			want:    []string{"@@||good.example.com^"},
		},
		{
			name:    "block is idempotent",
			current: []string{"||ads.example.com^"},
			domain:  "ads.example.com",
			block:   true,
			want:    []string{"||ads.example.com^"},
		},
		{
			name:    "block removes opposing allow rule",
			current: []string{"@@||ads.example.com^"},
			domain:  "ads.example.com",
			block:   true,
			want:    []string{"||ads.example.com^"},
		},
		{
			name:    "unblock removes opposing block rule",
			current: []string{"||ads.example.com^"},
			domain:  "ads.example.com",
			block:   false,
			want:    []string{"@@||ads.example.com^"},
		},
		{
			name:    "preserves unrelated rules in order",
			current: []string{"||keep.me^", "@@||other.org^", "||ads.example.com^"},
			domain:  "ads.example.com",
			block:   false,
			want:    []string{"||keep.me^", "@@||other.org^", "@@||ads.example.com^"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]string(nil), tc.current...)
			got := mergeDomainRule(tc.current, tc.domain, tc.block)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mergeDomainRule = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.current, before) {
				t.Errorf("input slice was mutated: %v", tc.current)
			}
		})
	}
}

func TestSetDomainBlockRoundTrip(t *testing.T) {
	fake := adguardtest.New("dns1", "u", "p", nil)
	srv := fake.Server()
	defer srv.Close()

	c := New(config.Instance{Name: "dns1", URL: srv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})
	ctx := context.Background()

	if err := c.SetDomainBlock(ctx, "ads.example.com", true); err != nil {
		t.Fatalf("block: %v", err)
	}
	if got := fake.Rules(); !reflect.DeepEqual(got, []string{"||ads.example.com^"}) {
		t.Fatalf("after block, rules = %v", got)
	}

	// Unblocking flips the same domain to an allow rule.
	if err := c.SetDomainBlock(ctx, "ads.example.com", false); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if got := fake.Rules(); !reflect.DeepEqual(got, []string{"@@||ads.example.com^"}) {
		t.Fatalf("after unblock, rules = %v", got)
	}
}

func TestSetDomainBlockPropagatesAPIError(t *testing.T) {
	fake := adguardtest.New("dns1", "u", "p", nil).WithFailures(adguardtest.Failures{Unauthate: true})
	srv := fake.Server()
	defer srv.Close()

	c := New(config.Instance{Name: "dns1", URL: srv.URL, Username: "u", Password: "p"},
		&http.Client{Timeout: 2 * time.Second})

	if err := c.SetDomainBlock(context.Background(), "ads.example.com", true); err == nil {
		t.Fatal("expected error when the instance rejects the request")
	}
}
