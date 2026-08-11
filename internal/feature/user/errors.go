package user

import (
	"net/http"

	"github.com/jarviisha/darkvoid/pkg/errors"
)

var (
	// User errors
	ErrUserNotFound      = errors.New("USER_NOT_FOUND", "User not found", http.StatusNotFound)
	ErrUserAlreadyExists = errors.New("USER_ALREADY_EXISTS", "User already exists", http.StatusConflict)

	// Authentication errors
	ErrInvalidCredentials  = errors.New("INVALID_CREDENTIALS", "Invalid credentials", http.StatusUnauthorized)
	ErrInvalidToken        = errors.New("INVALID_TOKEN", "Invalid token", http.StatusUnauthorized)
	ErrTokenExpired        = errors.New("TOKEN_EXPIRED", "Token expired", http.StatusUnauthorized)
	ErrAccountDisabled     = errors.New("ACCOUNT_DISABLED", "Account disabled", http.StatusForbidden)
	ErrInvalidRefreshToken = errors.New("INVALID_REFRESH_TOKEN", "Invalid refresh token", http.StatusUnauthorized)
	ErrRefreshTokenExpired = errors.New("REFRESH_TOKEN_EXPIRED", "Refresh token expired", http.StatusUnauthorized)
	ErrRefreshTokenRevoked = errors.New("REFRESH_TOKEN_REVOKED", "Refresh token revoked", http.StatusUnauthorized)

	// Password errors
	ErrWeakPassword = errors.New("WEAK_PASSWORD", "Password too weak", http.StatusBadRequest)
	// ErrUnsupportedImageType is returned when avatar or cover bytes are not a
	// supported JPEG or PNG image, regardless of the client-supplied filename.
	ErrUnsupportedImageType = errors.New("UNSUPPORTED_MEDIA_TYPE", "profile image must be JPEG or PNG", http.StatusUnsupportedMediaType)
	ErrProfileImageTooLarge = errors.New("FILE_TOO_LARGE", "profile image exceeds 10 MB", http.StatusRequestEntityTooLarge)

	// Role errors
	ErrRoleNotFound = errors.New("ROLE_NOT_FOUND", "Role not found", http.StatusNotFound)
)
