package aggregate

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{V: cursorVersion, I: map[string]InstanceCursor{
		"dns1": {O: "2026-07-10T00:00:00.5Z", D: false},
		"dns2": {O: "2026-07-10T00:00:01Z", D: true},
	}}
	got := DecodeCursor(c.Encode())
	if got == nil {
		t.Fatal("decode returned nil for valid cursor")
	}
	if got.I["dns1"].O != "2026-07-10T00:00:00.5Z" || got.I["dns2"].D != true {
		t.Errorf("round trip mismatch: %+v", got.I)
	}
}

func TestDecodeCursorEmptyIsPageOne(t *testing.T) {
	if DecodeCursor("") != nil {
		t.Error("empty string should decode to nil (page 1)")
	}
}

func TestDecodeCursorGarbage(t *testing.T) {
	for _, s := range []string{"!!!not-base64!!!", "YWJjZA", "eyJ2Ijp9"} {
		if got := DecodeCursor(s); got != nil {
			t.Errorf("garbage %q should decode to nil, got %+v", s, got)
		}
	}
}

func TestDecodeCursorWrongVersion(t *testing.T) {
	c := Cursor{V: 99, I: map[string]InstanceCursor{"dns1": {O: "x"}}}
	if got := DecodeCursor(c.Encode()); got != nil {
		t.Errorf("wrong version should decode to nil, got %+v", got)
	}
}

func TestDecodeCursorTamperNoPanic(t *testing.T) {
	c := Cursor{V: cursorVersion, I: map[string]InstanceCursor{"dns1": {O: "x"}}}
	enc := c.Encode()
	// Flip characters throughout; must never panic, only ever nil-or-valid.
	for i := 0; i < len(enc); i++ {
		mutated := enc[:i] + "0" + enc[i+1:]
		_ = DecodeCursor(mutated) // just must not panic
	}
}
