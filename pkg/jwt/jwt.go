package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Service handles JWT token operations
type Service struct {
	cfg Config
}

// NewService creates a new JWT service
func NewService(config Config) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Service{
		cfg: config,
	}, nil
}

// GenerateToken generates a JWT token with the given subject
func (s *Service) GenerateToken(subject string) (string, error) {
	now := time.Now()

	claims := &Claims{
		Issuer:    s.cfg.Issuer,
		Audience:  jwt.ClaimStrings{s.cfg.Audience},
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.Expiry)),
		NotBefore: jwt.NewNumericDate(now),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedToken, err := token.SignedString(s.cfg.Secret)
	if err != nil {
		return "", fmt.Errorf("jwt: failed to sign token: %w", err)
	}

	return signedToken, nil
}

// GenerateTokenWithClaims generates a JWT token with custom claims. The claims
// must satisfy the configured issuer, audience, expiration, temporal, and any
// custom validation requirements before they are signed.
func (s *Service) GenerateTokenWithClaims(claims jwt.Claims) (string, error) {
	if err := s.validateClaimsForIssuance(claims); err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedToken, err := token.SignedString(s.cfg.Secret)
	if err != nil {
		return "", fmt.Errorf("jwt: failed to sign token: %w", err)
	}

	return signedToken, nil
}

// ValidateToken validates a token and returns the claims
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := s.parseWithClaims(tokenString, &Claims{})

	if err != nil {
		return nil, s.mapError(err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ValidateTokenWithClaims validates a token with custom claims type
func (s *Service) ValidateTokenWithClaims(tokenString string, claims jwt.Claims) error {
	token, err := s.parseWithClaims(tokenString, claims)

	if err != nil {
		return s.mapError(err)
	}

	if !token.Valid {
		return ErrInvalidToken
	}

	return nil
}

func (s *Service) parseWithClaims(tokenString string, claims jwt.Claims) (*jwt.Token, error) {
	options := append(
		[]jwt.ParserOption{jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()})},
		s.claimsValidationOptions()...,
	)
	return jwt.ParseWithClaims(
		tokenString,
		claims,
		func(_ *jwt.Token) (any, error) {
			return s.cfg.Secret, nil
		},
		options...,
	)
}

func (s *Service) validateClaimsForIssuance(claims jwt.Claims) error {
	if claims == nil {
		return fmt.Errorf("%w: claims are required", ErrInvalidClaims)
	}
	if err := jwt.NewValidator(s.claimsValidationOptions()...).Validate(claims); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidClaims, err)
	}
	return nil
}

func (s *Service) claimsValidationOptions() []jwt.ParserOption {
	return []jwt.ParserOption{
		jwt.WithIssuer(s.cfg.Issuer),
		jwt.WithAudience(s.cfg.Audience),
		jwt.WithExpirationRequired(),
	}
}

// ParseToken parses a token without validating it (for debugging/inspection)
func (s *Service) ParseToken(tokenString string) (*Claims, error) {
	token, _, err := jwt.NewParser().ParseUnverified(tokenString, &Claims{})
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// GetExpiryDuration returns the configured token expiry duration
func (s *Service) GetExpiryDuration() time.Duration {
	return s.cfg.Expiry
}

// mapError maps JWT library errors to our custom errors
func (s *Service) mapError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrExpiredToken
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return ErrTokenNotYetValid
	case errors.Is(err, jwt.ErrTokenMalformed):
		return ErrInvalidToken
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return ErrInvalidToken
	default:
		return ErrInvalidToken
	}
}
