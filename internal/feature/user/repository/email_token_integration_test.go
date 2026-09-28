package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
)

// dsnEnv opts a run into the queries that need a real server; the double in
// mock_test.go proves the mapping but not the SQL. The test skips when it is
// unset, so `make test` is unaffected. Point it at a database with
// migrations/user applied:
//
//	DARKVOID_TEST_DB_DSN='postgres://user:pass@localhost:5432/db?sslmode=disable' \
//	  go test ./internal/feature/user/repository/ -run Claim -v
//
// Everything it writes happens inside a transaction that is rolled back, so it
// is safe to aim at a development database.
const dsnEnv = "DARKVOID_TEST_DB_DSN"

// The guarantee a reset link rests on: of two redemptions, exactly one wins,
// because the check and the write are the same statement.
func TestClaim_SecondRedemptionLoses(t *testing.T) {
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping the queries that need a server", dsnEnv)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	name := "claimtest_" + uuid.NewString()[:8]
	var userID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO usr.users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		name, name+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	repo := &EmailTokenRepository{queries: db.New(tx)}
	token, err := repo.Create(ctx, userID, uuid.NewString(), "reset_password", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	first, err := repo.Claim(ctx, token.ID)
	if err != nil || !first {
		t.Fatalf("first claim: got (%v, %v), want (true, nil)", first, err)
	}
	second, err := repo.Claim(ctx, token.ID)
	if err != nil || second {
		t.Fatalf("second claim: got (%v, %v), want (false, nil)", second, err)
	}
}
