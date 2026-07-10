// Package auth handles OIDC login and stateless, cookie-based sessions. No
// session data is stored server-side: the session is a sealed, authenticated
// cookie, consistent with the project's no-persistent-storage requirement.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// sessionCookieName is the cookie holding the sealed user session.
const sessionCookieName = "als_session"

// Session is the authenticated user identity carried in the session cookie.
type Session struct {
	Sub    string    `json:"sub"`
	Name   string    `json:"name"`
	Email  string    `json:"email"`
	Expiry time.Time `json:"exp"`
}

// Expired reports whether the session is past its expiry.
func (s Session) Expired(now time.Time) bool { return !now.Before(s.Expiry) }

// ErrExpired is returned by Open when a decoded session has passed its expiry.
var ErrExpired = errors.New("session expired")

// Codec seals and opens values using AES-256-GCM. The key is the SHA-256 of the
// configured session secret, so any secret length yields a valid 32-byte key.
type Codec struct {
	aead   cipher.AEAD
	secure bool
}

// NewCodec builds a Codec from the raw session secret. secure controls the
// Secure attribute on cookies it writes (false only for local HTTP dev).
func NewCodec(secret string, secure bool) (*Codec, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("session secret must be at least 32 characters")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Codec{aead: aead, secure: secure}, nil
}

// seal encrypts arbitrary bytes and returns a base64url token (nonce||ciphertext).
func (c *Codec) seal(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// open reverses seal, returning the plaintext or an error on any tampering.
func (c *Codec) open(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return nil, errors.New("sealed token too short")
	}
	nonce, ciphertext := raw[:ns], raw[ns:]
	return c.aead.Open(nil, nonce, ciphertext, nil)
}

// SealSession seals a session into an opaque cookie value.
func (c *Codec) SealSession(s Session) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return c.seal(raw)
}

// OpenSession opens and validates a session token, rejecting expired sessions.
func (c *Codec) OpenSession(token string, now time.Time) (Session, error) {
	raw, err := c.open(token)
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return Session{}, err
	}
	if s.Expired(now) {
		return Session{}, ErrExpired
	}
	return s, nil
}

// WriteSession seals the session and sets it as an HttpOnly cookie.
func (c *Codec) WriteSession(w http.ResponseWriter, s Session) error {
	token, err := c.SealSession(s)
	if err != nil {
		return err
	}
	// #nosec G124 -- Secure is driven by COOKIE_SECURE (default true); it is only
	// false for local HTTP development. HttpOnly and SameSite are always set.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  s.Expiry,
	})
	return nil
}

// ReadSession reads and validates the session cookie from a request.
func (c *Codec) ReadSession(r *http.Request, now time.Time) (Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return Session{}, err
	}
	return c.OpenSession(cookie.Value, now)
}

// ClearSession expires the session cookie on the client.
func (c *Codec) ClearSession(w http.ResponseWriter) {
	// #nosec G124 -- Secure is driven by COOKIE_SECURE (default true); it is only
	// false for local HTTP development. HttpOnly and SameSite are always set.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
