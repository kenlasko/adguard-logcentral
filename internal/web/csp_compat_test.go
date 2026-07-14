package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The security headers apply a strict CSP (script-src 'self', with no
// 'unsafe-inline' or 'unsafe-eval'). Two template patterns silently break under
// it, so guard against them:
//
//   - an inline <script> with a body is blocked outright (this is what stopped
//     the timezone localisation from running);
//   - an htmx bracket-filter trigger such as every 10s [expr] is compiled with
//     the Function constructor, which 'unsafe-eval' would be needed to allow, so
//     the filter fails and the poll fires unconditionally (this is what made
//     auto-refresh ignore its toggle).
//
// Keeping page behaviour in external 'self' scripts and driving conditional
// polling from JavaScript avoids both.

var inlineScriptWithBody = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>\s*\S`)

func TestTemplatesHaveNoInlineScriptBodies(t *testing.T) {
	entries, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		b, err := templateFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		body := string(b)
		// An external script tag (<script src=...></script>) is fine; only a
		// script element carrying its own code is CSP-blocked.
		for _, m := range regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).FindAllString(body, -1) {
			if strings.Contains(m, "src=") {
				continue
			}
			if inlineScriptWithBody.MatchString(m) {
				t.Errorf("%s contains an inline <script> with a body; move it to a "+
					"static file so the strict CSP does not block it:\n%s", name, m)
			}
		}
	}
}

// jsIndicators are characters and identifiers that appear in a JavaScript
// event-filter expression but not in the CSS attribute selectors that htmx
// modifiers such as from:/target: legitimately use (for example
// "input[name=search]"). Their presence inside a bracketed hx-trigger clause
// marks it as an eval-based filter.
var jsIndicators = []string{"document", "window", "this", "event", "(", ".", "&&", "||", "!", "==", "<", ">"}

func TestTemplatesHaveNoEvalHtmxFilters(t *testing.T) {
	entries, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	trigger := regexp.MustCompile(`hx-trigger="([^"]*)"`)
	bracket := regexp.MustCompile(`\[([^\]]*)\]`)
	for _, name := range entries {
		b, err := templateFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, tm := range trigger.FindAllStringSubmatch(string(b), -1) {
			for _, bm := range bracket.FindAllStringSubmatch(tm[1], -1) {
				for _, ind := range jsIndicators {
					if strings.Contains(bm[1], ind) {
						t.Errorf("%s uses an eval-based htmx trigger filter [%s]; drive "+
							"the condition from JavaScript instead so the strict CSP applies",
							name, bm[1])
						break
					}
				}
			}
		}
	}
}
