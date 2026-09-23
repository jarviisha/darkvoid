package jwt

import "time"

// Config holds JWT configuration
type Config struct {
	// Secret key for signing tokens
	Secret []byte

	// Issuer identifies the principal that issued the JWT
	Issuer string

	// Audience identifies the API that accepts the JWT
	Audience string

	// Expiry is the duration for access token validity
	Expiry time.Duration
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if len(c.Secret) == 0 {
		return ErrInvalidConfig
	}
	if c.Issuer == "" {
		return ErrInvalidConfig
	}
	if c.Audience == "" {
		return ErrInvalidConfig
	}
	if c.Expiry <= 0 {
		return ErrInvalidConfig
	}
	return nil
}
