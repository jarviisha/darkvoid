package jwt

import "errors"

var (
	// ErrInvalidConfig is returned when the JWT configuration is invalid
	ErrInvalidConfig = errors.New("jwt: invalid configuration")

	// ErrInvalidClaims is returned when claims cannot satisfy the configured
	// trust boundary and therefore must not be signed.
	ErrInvalidClaims = errors.New("jwt: invalid claims")

	// ErrInvalidToken is returned when the token is malformed or invalid
	ErrInvalidToken = errors.New("jwt: invalid token")

	// ErrExpiredToken is returned when the token has expired
	ErrExpiredToken = errors.New("jwt: token expired")

	// ErrTokenNotYetValid is returned when the token is not yet valid (nbf)
	ErrTokenNotYetValid = errors.New("jwt: token not yet valid")
)
