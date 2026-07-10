package adguard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatsUnmarshalEmptyTopLists(t *testing.T) {
	var s Stats
	if err := json.Unmarshal([]byte(`{"num_dns_queries":5}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.NumDNSQueries != 5 {
		t.Errorf("NumDNSQueries = %d", s.NumDNSQueries)
	}
	if len(s.TopQueriedDomains) != 0 {
		t.Errorf("expected empty top lists, got %+v", s.TopQueriedDomains)
	}
}

func TestStatsUnmarshalInvalid(t *testing.T) {
	var s Stats
	if err := json.Unmarshal([]byte(`{"num_dns_queries":"nope"}`), &s); err == nil {
		t.Error("expected error decoding malformed stats")
	}
}

func TestAPIErrorMessage(t *testing.T) {
	e := &APIError{Instance: "dns1", Path: "/status", Status: 503, Body: "down"}
	msg := e.Error()
	for _, want := range []string{"dns1", "/status", "503", "down"} {
		if !strings.Contains(msg, want) {
			t.Errorf("APIError message missing %q: %s", want, msg)
		}
	}
}
