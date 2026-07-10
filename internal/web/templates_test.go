package web

import (
	"testing"
	"time"

	"github.com/kenlasko/adguard-logcentral/internal/adguard"
)

func TestComma(t *testing.T) {
	cases := map[int64]string{0: "0", 42: "42", 1000: "1,000", 1234567: "1,234,567", -2500: "-2,500"}
	for in, want := range cases {
		if got := comma(in); got != want {
			t.Errorf("comma(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPercentAndMs(t *testing.T) {
	if got := percent1(25.0); got != "25.0%" {
		t.Errorf("percent1 = %q", got)
	}
	if got := ms3(0.0125); got != "12.50 ms" {
		t.Errorf("ms3 = %q", got)
	}
}

func TestReasonMapping(t *testing.T) {
	if reasonLabel("FilteredBlackList") != "Blocked" {
		t.Error("blacklist should label Blocked")
	}
	if reasonClass("FilteredBlackList") != "blocked" {
		t.Error("blacklist should class blocked")
	}
	if reasonClass("RewriteRule") != "rewritten" {
		t.Error("rewrite should class rewritten")
	}
	if reasonClass("NotFilteredNotFound") != "allowed" {
		t.Error("not-found should class allowed")
	}
	item := adguard.QueryLogItem{Reason: "FilteredBlackList"}
	if rowClass(item) != "row-blocked" {
		t.Errorf("rowClass = %q", rowClass(item))
	}
}

func TestLocalTimeAndMs(t *testing.T) {
	if localTime(time.Time{}) != "" {
		t.Error("zero time should render empty")
	}
	if formatMs("") != "" {
		t.Error("empty elapsed should render empty")
	}
	if got := formatMs("12"); got != "12.00 ms" {
		t.Errorf("formatMs(%q) = %q, want %q", "12", got, "12.00 ms")
	}
	if got := formatMs("1.23456"); got != "1.23 ms" {
		t.Errorf("formatMs(%q) = %q, want %q", "1.23456", got, "1.23 ms")
	}
	if got := formatMs("n/a"); got != "n/a ms" {
		t.Errorf("formatMs(%q) = %q, want %q", "n/a", got, "n/a ms")
	}
}

func TestDict(t *testing.T) {
	m, err := dict("Title", "X", "Rows", 3)
	if err != nil {
		t.Fatal(err)
	}
	if m["Title"] != "X" || m["Rows"] != 3 {
		t.Errorf("dict = %v", m)
	}
	if _, err := dict("odd"); err == nil {
		t.Error("odd argument count should error")
	}
	if _, err := dict(1, "v"); err == nil {
		t.Error("non-string key should error")
	}
}
