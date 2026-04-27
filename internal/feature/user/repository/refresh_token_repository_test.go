package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
)

func TestDbRefreshTokenToEntity_NoNullableFields(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Microsecond)
	created := time.Now().UTC().Truncate(time.Microsecond)

	dbToken := db.UsrRefreshToken{
		ID:        id,
		Token:     "refresh-abc",
		UserID:    userID,
		IsRevoked: false,
		ExpiresAt: pgtype.Timestamp{Time: expires, Valid: true},
		CreatedAt: pgtype.Timestamp{Time: created, Valid: true},
	}

	token := dbRefreshTokenToEntity(dbToken)

	if token.ID != id {
		t.Errorf("ID mismatch")
	}
	if token.UserID != userID {
		t.Errorf("UserID mismatch")
	}
	if token.Token != "refresh-abc" {
		t.Errorf("Token mismatch")
	}
	if token.IsRevoked {
		t.Errorf("IsRevoked should be false")
	}
	if !token.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt mismatch: want %v got %v", expires, token.ExpiresAt)
	}
	if token.RevokedAt != nil {
		t.Errorf("RevokedAt should be nil")
	}
}

func TestDbRefreshTokenToEntity_RevokedAtSet(t *testing.T) {
	revokedAt := time.Now().UTC().Truncate(time.Microsecond)
	dbToken := db.UsrRefreshToken{
		IsRevoked: true,
		RevokedAt: pgtype.Timestamp{Time: revokedAt, Valid: true},
	}

	token := dbRefreshTokenToEntity(dbToken)

	if token.RevokedAt == nil {
		t.Fatal("RevokedAt should not be nil")
	}
	if !token.RevokedAt.Equal(revokedAt) {
		t.Errorf("RevokedAt mismatch: want %v got %v", revokedAt, *token.RevokedAt)
	}
	if !token.IsRevoked {
		t.Errorf("IsRevoked should be true")
	}
}

func TestDbRefreshTokenToEntity_ExpiresAtNotSet(t *testing.T) {
	dbToken := db.UsrRefreshToken{
		Token: "tok",
		// ExpiresAt left invalid
	}

	token := dbRefreshTokenToEntity(dbToken)

	if !token.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt should be zero when DB field invalid, got %v", token.ExpiresAt)
	}
}
