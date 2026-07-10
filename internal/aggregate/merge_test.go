package aggregate

import (
	"testing"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
)

func item(instance, raw string) adguard.QueryLogItem {
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return adguard.QueryLogItem{Instance: instance, RawTime: raw, ParsedTime: t}
}

func TestMergeDescendingOrdering(t *testing.T) {
	groups := []taggedEntries{
		{Instance: "a", Entries: []adguard.QueryLogItem{
			item("a", "2026-07-10T00:00:05Z"),
			item("a", "2026-07-10T00:00:02Z"),
		}},
		{Instance: "b", Entries: []adguard.QueryLogItem{
			item("b", "2026-07-10T00:00:04Z"),
			item("b", "2026-07-10T00:00:03Z"),
		}},
	}
	res := mergeDescending(groups, 10)
	want := []string{
		"2026-07-10T00:00:05Z",
		"2026-07-10T00:00:04Z",
		"2026-07-10T00:00:03Z",
		"2026-07-10T00:00:02Z",
	}
	if len(res.Page) != len(want) {
		t.Fatalf("got %d entries, want %d", len(res.Page), len(want))
	}
	for i, w := range want {
		if res.Page[i].RawTime != w {
			t.Errorf("position %d = %q, want %q", i, res.Page[i].RawTime, w)
		}
	}
}

func TestMergeDescendingTieBreak(t *testing.T) {
	// Identical timestamp across instances: deterministic by instance name.
	same := "2026-07-10T00:00:00Z"
	groups := []taggedEntries{
		{Instance: "b", Entries: []adguard.QueryLogItem{item("b", same)}},
		{Instance: "a", Entries: []adguard.QueryLogItem{item("a", same)}},
	}
	res := mergeDescending(groups, 10)
	if res.Page[0].Instance != "a" || res.Page[1].Instance != "b" {
		t.Errorf("tie-break order = %q,%q; want a,b", res.Page[0].Instance, res.Page[1].Instance)
	}
}

func TestMergeDescendingTrimAndConsumed(t *testing.T) {
	groups := []taggedEntries{
		{Instance: "a", Entries: []adguard.QueryLogItem{
			item("a", "2026-07-10T00:00:05Z"),
			item("a", "2026-07-10T00:00:01Z"),
		}},
		{Instance: "b", Entries: []adguard.QueryLogItem{
			item("b", "2026-07-10T00:00:04Z"),
			item("b", "2026-07-10T00:00:03Z"),
		}},
	}
	res := mergeDescending(groups, 3)
	if len(res.Page) != 3 {
		t.Fatalf("trim failed: got %d, want 3", len(res.Page))
	}
	// Newest 3 are a@05, b@04, b@03: a consumes 1, b consumes 2.
	if res.Consumed["a"] != 1 || res.Consumed["b"] != 2 {
		t.Errorf("consumed = %v, want a:1 b:2", res.Consumed)
	}
}

func TestMergeDoesNotMutateInputs(t *testing.T) {
	entries := []adguard.QueryLogItem{
		item("a", "2026-07-10T00:00:01Z"),
		item("a", "2026-07-10T00:00:05Z"),
	}
	groups := []taggedEntries{{Instance: "a", Entries: entries}}
	_ = mergeDescending(groups, 10)
	// Original slice order must be untouched.
	if entries[0].RawTime != "2026-07-10T00:00:01Z" {
		t.Error("mergeDescending mutated its input slice")
	}
}
