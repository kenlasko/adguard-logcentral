package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testCodec(t *testing.T) *Codec {
	t.Helper()
	c, err := NewCodec(strings.Repeat("k", 32), true)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	return c
}

func TestSessionRoundTrip(t *testing.T) {
	c := testCodec(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	sess := Session{Sub: "user-1", Name: "Ada", Email: "ada@example.com", Expiry: now.Add(time.Hour)}

	token, err := c.SealSession(sess)
	if err != nil {
		t.Fatalf("SealSession: %v", err)
	}
	got, err := c.OpenSession(token, now)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got.Sub != "user-1" || got.Name != "Ada" || got.Email != "ada@example.com" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestSessionExpired(t *testing.T) {
	c := testCodec(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	sess := Session{Sub: "u", Expiry: now.Add(-time.Second)}
	token, _ := c.SealSession(sess)

	_, err := c.OpenSession(token, now)
	if !errors.Is(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestSessionTamperRejected(t *testing.T) {
	c := testCodec(t)
	now := time.Now()
	token, _ := c.SealSession(Session{Sub: "u", Expiry: now.Add(time.Hour)})

	// Flip a byte in the middle of the token; GCM must reject it.
	mid := len(token) / 2
	tampered := token[:mid] + flip(token[mid]) + token[mid+1:]
	if _, err := c.OpenSession(tampered, now); err == nil {
		t.Error("tampered token should not open")
	}
}

func TestSessionWrongKeyRejected(t *testing.T) {
	c1 := testCodec(t)
	c2, _ := NewCodec(strings.Repeat("z", 40), true)
	now := time.Now()
	token, _ := c1.SealSession(Session{Sub: "u", Expiry: now.Add(time.Hour)})

	if _, err := c2.OpenSession(token, now); err == nil {
		t.Error("token sealed with a different key should not open")
	}
}

func TestNewCodecRejectsShortSecret(t *testing.T) {
	if _, err := NewCodec("short", true); err == nil {
		t.Error("expected error for short secret")
	}
}

func TestCookieRoundTripThroughHTTP(t *testing.T) {
	c := testCodec(t)
	now := time.Now()
	sess := Session{Sub: "u", Name: "N", Expiry: now.Add(time.Hour)}

	rec := httptest.NewRecorder()
	if err := c.WriteSession(rec, sess); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || !cookie.Secure {
		t.Errorf("cookie attributes wrong: %+v", cookie)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	got, err := c.ReadSession(req, now)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got.Sub != "u" {
		t.Errorf("read session sub = %q", got.Sub)
	}
}

func flip(b byte) string {
	if b == 'A' {
		return "B"
	}
	return "A"
}
