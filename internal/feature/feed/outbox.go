package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const outboxMaxAttempts = 10

type outboxEntry struct {
	ID       uuid.UUID
	Event    Event
	Attempts int
}

// PostgresOutbox durably stores feed events in the same transaction as the
// post or follow mutation that produced them.
type PostgresOutbox struct{ pool *pgxpool.Pool }

func NewPostgresOutbox(pool *pgxpool.Pool) *PostgresOutbox { return &PostgresOutbox{pool: pool} }

func (o *PostgresOutbox) Enqueue(ctx context.Context, tx pgx.Tx, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal feed outbox event: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO usr.feed_outbox (event) VALUES ($1)`, payload); err != nil {
		return fmt.Errorf("insert feed outbox event: %w", err)
	}
	return nil
}

func (o *PostgresOutbox) claim(ctx context.Context, limit int) ([]outboxEntry, error) {
	rows, err := o.pool.Query(ctx, `
WITH picked AS (
    SELECT id
    FROM usr.feed_outbox
    WHERE dead_lettered_at IS NULL AND available_at <= NOW()
    ORDER BY created_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE usr.feed_outbox AS outbox
SET attempts = outbox.attempts + 1,
    available_at = NOW() + INTERVAL '30 seconds'
FROM picked
WHERE outbox.id = picked.id
RETURNING outbox.id, outbox.event, outbox.attempts`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim feed outbox events: %w", err)
	}
	defer rows.Close()
	entries := make([]outboxEntry, 0, limit)
	for rows.Next() {
		var entry outboxEntry
		var payload []byte
		if err := rows.Scan(&entry.ID, &payload, &entry.Attempts); err != nil {
			return nil, fmt.Errorf("scan feed outbox event: %w", err)
		}
		if err := json.Unmarshal(payload, &entry.Event); err != nil {
			return nil, fmt.Errorf("decode feed outbox event %s: %w", entry.ID, err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feed outbox events: %w", err)
	}
	return entries, nil
}

func (o *PostgresOutbox) complete(ctx context.Context, id uuid.UUID) error {
	_, err := o.pool.Exec(ctx, `DELETE FROM usr.feed_outbox WHERE id = $1`, id)
	return err
}

func (o *PostgresOutbox) fail(ctx context.Context, entry outboxEntry, eventErr error) error {
	backoff := min(time.Duration(1<<min(entry.Attempts, 8))*time.Second, 5*time.Minute)
	_, err := o.pool.Exec(ctx, `
UPDATE usr.feed_outbox
SET last_error = $2,
    available_at = NOW() + $3::interval,
    dead_lettered_at = CASE WHEN attempts >= $4 THEN NOW() ELSE NULL END
WHERE id = $1`, entry.ID, eventErr.Error(), backoff.String(), outboxMaxAttempts)
	return err
}
