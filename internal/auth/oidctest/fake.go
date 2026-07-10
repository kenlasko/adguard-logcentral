// Package oidctest provides a self-contained fake OIDC issuer for exercising
// the login-to-callback flow without a real identity provider. It serves
// discovery, a JWKS built from a generated RSA key, an authorize endpoint that
// round-trips state and nonce, and a token endpoint that mints signed ID
// tokens. Knobs allow injecting a wrong nonce or audience.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"
)

const keyID = "test-key"

// Issuer is a fake OIDC provider. Configure the identity fields and knobs
// before driving a flow; the zero value is not usable (use New).
type Issuer struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	ClientID string
	Subject  string
	Name     string
	Email    string

	// ForceNonce, when non-empty, overrides the nonce placed in the ID token,
	// to test nonce-mismatch rejection.
	ForceNonce string
	// ForceAudience, when non-empty, overrides the token audience, to test
	// audience-mismatch rejection.
	ForceAudience string

	mu    sync.Mutex
	codes map[string]string // authorization code -> nonce
}

// New starts a fake issuer with the given client ID and identity claims.
func New(clientID, subject, name, email string) *Issuer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	iss := &Issuer{
		key:      key,
		ClientID: clientID,
		Subject:  subject,
		Name:     name,
		Email:    email,
		codes:    map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", iss.handleDiscovery)
	mux.HandleFunc("GET /jwks", iss.handleJWKS)
	mux.HandleFunc("GET /authorize", iss.handleAuthorize)
	mux.HandleFunc("POST /token", iss.handleToken)
	iss.server = httptest.NewServer(mux)
	return iss
}

// URL returns the issuer base URL (its "iss" value).
func (i *Issuer) URL() string { return i.server.URL }

// Close shuts down the issuer.
func (i *Issuer) Close() { i.server.Close() }

func (i *Issuer) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                i.server.URL,
		"authorization_endpoint":                i.server.URL + "/authorize",
		"token_endpoint":                        i.server.URL + "/token",
		"jwks_uri":                              i.server.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (i *Issuer) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := i.key.PublicKey
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": keyID,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

// handleAuthorize records the request nonce against a fresh code and redirects
// back to the client redirect_uri, mirroring the real authorization endpoint.
func (i *Issuer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	nonce := q.Get("nonce")

	code := randString()
	i.mu.Lock()
	i.codes[code] = nonce
	i.mu.Unlock()

	dest, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	rq := dest.Query()
	rq.Set("code", code)
	rq.Set("state", state)
	dest.RawQuery = rq.Encode()
	http.Redirect(w, r, dest.String(), http.StatusFound)
}

func (i *Issuer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := r.Form.Get("code")
	i.mu.Lock()
	nonce, ok := i.codes[code]
	delete(i.codes, code)
	i.mu.Unlock()
	if !ok {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	if i.ForceNonce != "" {
		nonce = i.ForceNonce
	}
	aud := i.ClientID
	if i.ForceAudience != "" {
		aud = i.ForceAudience
	}

	idToken := i.signIDToken(nonce, aud)
	writeJSON(w, map[string]any{
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

// signIDToken builds and RS256-signs an ID token with the configured claims.
func (i *Issuer) signIDToken(nonce, aud string) string {
	now := time.Now()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID}
	claims := map[string]any{
		"iss":   i.server.URL,
		"sub":   i.Subject,
		"aud":   aud,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
		"nonce": nonce,
		"name":  i.Name,
		"email": i.Email,
	}
	signingInput := encodeSegment(header) + "." + encodeSegment(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func encodeSegment(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func randString() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
