package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

// flowCookieName holds the short-lived OIDC flow state (state, nonce, PKCE
// verifier, return-to) between /auth/login and /auth/callback.
const flowCookieName = "als_oidc_flow"

// flowTTL bounds how long a login attempt may take.
const flowTTL = 10 * time.Minute

// flowState is the sealed, short-lived state carried across the OIDC redirect.
type flowState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
}

// Authenticator wires the OIDC login flow to the session codec. It is
// constructed once at startup (performing provider discovery) and its handlers
// are mounted under /auth/*.
type Authenticator struct {
	oauth2Config oauth2.Config
	verifier     IDTokenVerifier
	codec        *Codec
	sessionDur   time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

// oidcVerifier adapts go-oidc's verifier to the IDTokenVerifier seam.
type oidcVerifier struct {
	v *oidc.IDTokenVerifier
}

func (o oidcVerifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	tok, err := o.v.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return nil, err
	}
	c.Subject = tok.Subject
	c.Nonce = tok.Nonce
	return &c, nil
}

// NewAuthenticator performs OIDC discovery against the configured issuer and
// returns a ready Authenticator. ctx bounds the discovery request.
func NewAuthenticator(ctx context.Context, cfg config.Config, codec *Codec, logger *slog.Logger) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, err
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})
	return &Authenticator{
		oauth2Config: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier:   oidcVerifier{v: verifier},
		codec:      codec,
		sessionDur: cfg.SessionDuration,
		logger:     logger,
		now:        time.Now,
	}, nil
}

// Routes registers the auth endpoints on the given mux.
func (a *Authenticator) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("GET /auth/logout", a.handleLogout)
}

func (a *Authenticator) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := randomToken()
	nonce := randomToken()
	verifier := oauth2.GenerateVerifier()

	flow := flowState{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		ReturnTo: safeReturnTo(r.URL.Query().Get("return")),
	}
	if err := a.writeFlowCookie(w, flow); err != nil {
		a.logger.Error("failed to seal oidc flow cookie", "error", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}

	authURL := a.oauth2Config.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *Authenticator) handleCallback(w http.ResponseWriter, r *http.Request) {
	flow, err := a.readFlowCookie(r)
	if err != nil {
		a.logger.Warn("oidc callback without valid flow cookie", "error", err)
		http.Error(w, "login session expired, please try again", http.StatusBadRequest)
		return
	}
	a.clearFlowCookie(w)

	if q := r.URL.Query().Get("state"); q == "" || q != flow.State {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "identity provider returned an error: "+errParam, http.StatusUnauthorized)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	token, err := a.oauth2Config.Exchange(r.Context(), code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		a.logger.Warn("oidc code exchange failed", "error", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		http.Error(w, "no id_token in response", http.StatusUnauthorized)
		return
	}

	claims, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		a.logger.Warn("id token verification failed", "error", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	if claims.Nonce != flow.Nonce {
		http.Error(w, "nonce mismatch", http.StatusUnauthorized)
		return
	}

	sess := Session{
		Sub:    claims.Subject,
		Name:   claims.Name,
		Email:  claims.Email,
		Expiry: a.now().Add(a.sessionDur),
	}
	if err := a.codec.WriteSession(w, sess); err != nil {
		a.logger.Error("failed to write session", "error", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, flow.ReturnTo, http.StatusFound)
}

func (a *Authenticator) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.codec.ClearSession(w)
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

func (a *Authenticator) writeFlowCookie(w http.ResponseWriter, flow flowState) error {
	raw, err := json.Marshal(flow)
	if err != nil {
		return err
	}
	sealed, err := a.codec.seal(raw)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    sealed,
		Path:     "/auth/",
		HttpOnly: true,
		Secure:   a.codec.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  a.now().Add(flowTTL),
	})
	return nil
}

func (a *Authenticator) readFlowCookie(r *http.Request) (flowState, error) {
	cookie, err := r.Cookie(flowCookieName)
	if err != nil {
		return flowState{}, err
	}
	raw, err := a.codec.open(cookie.Value)
	if err != nil {
		return flowState{}, err
	}
	var flow flowState
	if err := json.Unmarshal(raw, &flow); err != nil {
		return flowState{}, err
	}
	return flow, nil
}

func (a *Authenticator) clearFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    "",
		Path:     "/auth/",
		HttpOnly: true,
		Secure:   a.codec.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// randomToken returns a URL-safe 256-bit random string for state and nonce.
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// safeReturnTo restricts the post-login redirect to local, absolute paths so an
// attacker cannot craft an open redirect via the return parameter.
func safeReturnTo(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
		return "/"
	}
	return v
}
