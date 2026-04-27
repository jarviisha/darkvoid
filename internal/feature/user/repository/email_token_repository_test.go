package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

func TestDbEmailTokenToEntity_UsedAtNil(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	created := time.Now().UTC().Truncate(time.Microsecond)

	row := db.UsrEmailToken{
		ID:        id,
		UserID:    userID,
		Token:     "tok123",
		Type:      string(entity.EmailTokenVerify),
		ExpiresAt: pgtype.Timestamp{Time: expires, Valid: true},
		CreatedAt: pgtype.Timestamp{Time: created, Valid: true},
		// UsedAt left as zero value (invalid)
	}

	et := dbEmailTokenToEntity(row)

	if et.ID != id {
		t.Errorf("ID mismatch")
	}
	if et.UserID != userID {
		t.Errorf("UserID mismatch")
	}
	if et.Token != "tok123" {
		t.Errorf("Token mismatch")
	}
	if et.Type != entity.EmailTokenVerify {
		t.Errorf("Type mismatch: want %v got %v", entity.EmailTokenVerify, et.Type)
	}
	if !et.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt mismatch")
	}
	if et.UsedAt != nil {
		t.Errorf("UsedAt should be nil when DB field invalid")
	}
}

func TestDbEmailTokenToEntity_UsedAtSet(t *testing.T) {
	usedAt := time.Now().UTC().Truncate(time.Microsecond)
	row := db.UsrEmailToken{
		Type:   string(entity.EmailTokenResetPassword),
		UsedAt: pgtype.Timestamp{Time: usedAt, Valid: true},
	}

	et := dbEmailTokenToEntity(row)

	if et.UsedAt == nil {
		t.Fatal("UsedAt should not be nil")
	}
	if !et.UsedAt.Equal(usedAt) {
		t.Errorf("UsedAt mismatch: want %v got %v", usedAt, *et.UsedAt)
	}
	if et.Type != entity.EmailTokenResetPassword {
		t.Errorf("Type mismatch")
	}
}
