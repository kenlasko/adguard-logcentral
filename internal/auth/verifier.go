package auth

import "context"

// Claims are the subset of ID token claims this application consumes.
type Claims struct {
	Subject string `json:"sub"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Nonce   string `json:"nonce"`
}

// IDTokenVerifier verifies a raw ID token and returns its claims. It is a small
// seam so middleware and handler tests can substitute a fake and never need a
// live issuer. The production implementation wraps go-oidc's verifier.
type IDTokenVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (*Claims, error)
}
