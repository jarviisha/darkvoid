package jwt

import (
	"github.com/golang-jwt/jwt/v5"
)

// Claims is an alias for standard JWT registered claims
// Users can extend this in their own code if they need custom claims
type Claims = jwt.RegisteredClaims

// NewClaims creates subject-only claims. Callers using GenerateTokenWithClaims
// must also populate the service's issuer, audience, and expiration boundary.
func NewClaims(subject string) *Claims {
	return &Claims{
		Subject: subject,
	}
}
