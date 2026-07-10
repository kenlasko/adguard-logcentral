package buildinfo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGetReturnsDefaults(t *testing.T) {
	got := Get()
	if got.Version == "" {
		t.Error("Version should never be empty")
	}
	if got.Commit != Commit {
		t.Errorf("Commit = %q, want %q", got.Commit, Commit)
	}
	if got.Date != Date {
		t.Errorf("Date = %q, want %q", got.Date, Date)
	}
}

func TestStringIncludesEveryField(t *testing.T) {
	info := Info{Version: "1.2.3", Commit: "abc1234", Date: "2026-07-10T12:00:00Z"}
	s := info.String()
	for _, want := range []string{"1.2.3", "abc1234", "2026-07-10T12:00:00Z"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}

func TestInfoJSONShape(t *testing.T) {
	b, err := json.Marshal(Info{Version: "1.2.3", Commit: "abc", Date: "d"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"version", "commit", "date"} {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON missing key %q in %s", k, b)
		}
	}
}
