package middleware

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/jwt"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// AuthMiddleware bundles the two auth middleware variants so that bounded contexts
// can apply authentication without knowing about the JWT implementation.
type AuthMiddleware struct {
	Required func(http.Handler) http.Handler // 401 when token is missing/invalid
	Optional func(http.Handler) http.Handler // continues without auth on failure
}

// NewAuthMiddleware creates an AuthMiddleware from a JWT service.
func NewAuthMiddleware(jwtService *jwt.Service) AuthMiddleware {
	return AuthMiddleware{
		Required: Auth(jwtService),
		Optional: OptionalAuth(jwtService),
	}
}

// extractToken extracts a JWT from the Authorization header. Credentials are
// never accepted from the URL because query strings leak into logs, history,
// referrers, and monitoring systems.
func extractToken(r *http.Request) string {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if token := strings.TrimPrefix(authHeader, "Bearer "); token != authHeader {
			return token
		}
	}
	return ""
}

// Auth creates an authentication middleware that validates JWT tokens.
// Accepts tokens only from the Authorization header.
func Auth(jwtService *jwt.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract the bearer token from the Authorization header.
			tokenString := extractToken(r)
			if tokenString == "" {
				logger.Warn(ctx, "missing authorization token")
				errors.WriteErrorResponse(w, errors.NewUnauthorizedError("missing authorization token"))
				return
			}

			// Validate token
			claims, err := jwtService.ValidateToken(tokenString)
			if err != nil {
				logger.Warn(ctx, "token validation failed", "error", err)

				switch err {
				case jwt.ErrExpiredToken:
					errors.WriteErrorResponse(w, errors.NewUnauthorizedError("token expired"))
				case jwt.ErrInvalidToken:
					errors.WriteErrorResponse(w, errors.NewUnauthorizedError("invalid token"))
				case jwt.ErrTokenNotYetValid:
					errors.WriteErrorResponse(w, errors.NewUnauthorizedError("token not yet valid"))
				default:
					errors.WriteErrorResponse(w, errors.ErrUnauthorized)
				}
				return
			}

			// Validate subject (user ID) exists
			if claims.Subject == "" {
				logger.Warn(ctx, "token missing subject")
				errors.WriteErrorResponse(w, errors.NewUnauthorizedError("invalid token claims"))
				return
			}

			// Parse user ID from subject
			userID, err := uuid.Parse(claims.Subject)
			if err != nil {
				logger.Warn(ctx, "invalid user ID format in token", "subject", claims.Subject, "error", err)
				errors.WriteErrorResponse(w, errors.NewUnauthorizedError("invalid token claims"))
				return
			}

			// Store user ID in context and enrich the context logger with user_id
			// so subsequent logs within this request carry the field automatically.
			ctx = httputil.WithUserID(ctx, userID)
			ctx = logger.WithUserID(ctx, userID.String())

			// Call next handler with updated context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth creates an optional authentication middleware.
// If token is present and valid, user info is stored in context.
// If token is missing or invalid, request continues without authentication.
// Accepts tokens only from the Authorization header.
func OptionalAuth(jwtService *jwt.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			tokenString := extractToken(r)
			if tokenString == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Validate token
			claims, err := jwtService.ValidateToken(tokenString)
			if err != nil {
				// Invalid token, continue without authentication
				logger.Debug(ctx, "optional auth: token validation failed", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			// Store user ID in context if valid
			if claims.Subject != "" {
				userID, err := uuid.Parse(claims.Subject)
				if err == nil {
					ctx = httputil.WithUserID(ctx, userID)
					ctx = logger.WithUserID(ctx, userID.String())
					logger.Debug(ctx, "optional auth: user authenticated")
				} else {
					logger.Debug(ctx, "optional auth: invalid user ID format", "subject", claims.Subject)
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
