package web

import "strings"

// sanitizeLogValue strips carriage returns and line feeds from a
// user-provided value before it is written to a log entry. Without this a
// value containing newlines could forge additional log lines (CWE-117).
// Callers already validate these values, so this is defense-in-depth that
// also keeps a single log record on a single line.
func sanitizeLogValue(v string) string {
	return strings.NewReplacer("\n", "", "\r", "").Replace(v)
}
