package web

import (
	"embed"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/adguard"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// partialFiles are define-only templates (unique names) safe to include in
// every page set and to render standalone as htmx fragments.
var partialFiles = []string{
	"templates/logs_rows.html",
	"templates/stats_panels.html",
	"templates/health_bar.html",
}

// pageFiles are the full-page templates; each defines its own "content" and
// "title" blocks, so each is parsed into an isolated template set to avoid
// block-name collisions.
var pageFiles = []string{"logs.html", "stats.html"}

// templates holds the parsed page sets plus a fragments set for htmx partials.
type templates struct {
	pages     map[string]*template.Template
	fragments *template.Template
}

// newTemplates parses all embedded templates, wiring in the shared FuncMap.
func newTemplates() (*templates, error) {
	fm := funcMap()

	pages := make(map[string]*template.Template, len(pageFiles))
	for _, page := range pageFiles {
		files := append([]string{"templates/layout.html", "templates/" + page}, partialFiles...)
		t, err := template.New("layout.html").Funcs(fm).ParseFS(templateFS, files...)
		if err != nil {
			return nil, fmt.Errorf("parse page %s: %w", page, err)
		}
		pages[strings.TrimSuffix(page, ".html")] = t
	}

	fragments, err := template.New("fragments").Funcs(fm).ParseFS(templateFS, partialFiles...)
	if err != nil {
		return nil, fmt.Errorf("parse fragments: %w", err)
	}
	return &templates{pages: pages, fragments: fragments}, nil
}

// funcMap holds the presentation helpers referenced by templates.
func funcMap() template.FuncMap {
	return template.FuncMap{
		"localTime":   localTime,
		"reasonLabel": reasonLabel,
		"reasonClass": reasonClass,
		"rowClass":    rowClass,
		"formatMs":    formatMs,
		"comma":       comma,
		"percent1":    percent1,
		"ms3":         ms3,
		"dict":        dict,
	}
}

// dict builds a map from alternating key/value template arguments, letting a
// caller pass multiple named values into a sub-template invocation.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict requires an even number of arguments")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

// localTime formats a query log timestamp for display in the server's local
// zone. It accepts the parsed time to avoid re-parsing the raw string.
func localTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// reasonLabel maps AdGuard's reason codes to short human labels.
func reasonLabel(reason string) string {
	switch reason {
	case "NotFilteredNotFound", "NotFilteredWhiteList":
		return "Allowed"
	case "FilteredBlackList":
		return "Blocked"
	case "FilteredSafeBrowsing":
		return "Safe Browsing"
	case "FilteredParental":
		return "Parental"
	case "FilteredInvalid":
		return "Invalid"
	case "FilteredSafeSearch":
		return "Safe Search"
	case "Rewrite", "RewriteEtcHosts", "RewriteRule":
		return "Rewritten"
	default:
		return reason
	}
}

// reasonClass returns a CSS class for a reason, driving row color-coding.
func reasonClass(reason string) string {
	switch reason {
	case "FilteredBlackList", "FilteredSafeBrowsing", "FilteredParental", "FilteredInvalid", "FilteredSafeSearch":
		return "blocked"
	case "Rewrite", "RewriteEtcHosts", "RewriteRule":
		return "rewritten"
	default:
		return "allowed"
	}
}

// rowClass color-codes a whole row from its item's reason.
func rowClass(item adguard.QueryLogItem) string {
	return "row-" + reasonClass(item.Reason)
}

// formatMs renders AdGuard's elapsedMs string (already milliseconds) compactly.
func formatMs(elapsed string) string {
	if elapsed == "" {
		return ""
	}
	return elapsed + " ms"
}

// comma formats an integer with thousands separators.
func comma(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	out = append([]string{s}, out...)
	res := strings.Join(out, ",")
	if neg {
		return "-" + res
	}
	return res
}

// percent1 formats a percentage to one decimal place.
func percent1(v float64) string { return fmt.Sprintf("%.1f%%", v) }

// ms3 renders a processing time given in seconds as milliseconds.
func ms3(seconds float64) string { return fmt.Sprintf("%.2f ms", seconds*1000) }
