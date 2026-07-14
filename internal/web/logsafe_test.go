package web

import "testing"

func TestSanitizeLogValue(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"plain":               "plain",
		"a\nb":                "ab",
		"a\rb":                "ab",
		"a\r\nb":              "ab",
		"line1\nforged=admin": "line1forged=admin",
		"tabs\tkept":          "tabs\tkept",
	}
	for in, want := range cases {
		if got := sanitizeLogValue(in); got != want {
			t.Errorf("sanitizeLogValue(%q) = %q, want %q", in, got, want)
		}
	}
}
