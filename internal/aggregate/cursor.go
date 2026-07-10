// Package aggregate fans query log, stats, and status requests out across all
// configured AdGuard instances and merges the results. The logs pagination
// algorithm (composite cursor + merge) is the highest-risk logic in the
// project and is covered exhaustively by tests.
package aggregate

import (
	"encoding/base64"
	"encoding/json"
)

// cursorVersion is bumped if the wire format changes; a mismatch is treated as
// "start from page 1" rather than an error.
const cursorVersion = 1

// InstanceCursor is the per-instance pagination state carried across a
// load-more request.
type InstanceCursor struct {
	// O is the raw "time" string of the last entry already served from this
	// instance; it becomes the next older_than value. Preserved verbatim.
	O string `json:"o"`
	// D marks the instance as exhausted (done); it is skipped on later pages.
	D bool `json:"d"`
}

// Cursor is the composite pagination state across all instances.
type Cursor struct {
	V int                       `json:"v"`
	I map[string]InstanceCursor `json:"i"`
}

// Encode serializes the cursor to a URL-safe base64 string suitable for
// embedding in an htmx load-more request.
func (c Cursor) Encode() string {
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses an encoded cursor. It returns nil (meaning "page 1") for
// empty input, undecodable input, or a version mismatch. It never panics on
// adversarial input.
func DecodeCursor(s string) *Cursor {
	if s == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil
	}
	if c.V != cursorVersion {
		return nil
	}
	if c.I == nil {
		c.I = map[string]InstanceCursor{}
	}
	return &c
}
