package repository

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// Mentions come back in the order the author wrote them, which nothing but the
// position column records: every mention of one post shares one created_at,
// and without an ORDER BY the server returns whatever its plan yields.
//
// Rows are inserted out of written order, and written order is not uuid order,
// so neither heap order nor primary-key order can pass for it. Unlike the
// search tests this one writes, but only inside a transaction it rolls back.
func TestMentionGetBatch_ReturnsWrittenOrder(t *testing.T) {
	tx := beginRollbackTx(t)
	ctx := context.Background()
	postA, postB := insertTestPost(t, tx), insertTestPost(t, tx)
	writtenA, writtenB := descendingUUIDs(4), descendingUUIDs(3)

	r := NewMentionRepository(nil).WithTx(tx)
	insertOutOfOrder(t, writtenA, func(uid uuid.UUID, pos int32) error { return r.Insert(ctx, postA, uid, pos) })
	insertOutOfOrder(t, writtenB, func(uid uuid.UUID, pos int32) error { return r.Insert(ctx, postB, uid, pos) })

	got, err := r.GetBatch(ctx, []uuid.UUID{postA, postB})
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	assertOrder(t, "post A", writtenA, got[postA])
	assertOrder(t, "post B", writtenB, got[postB])
}

func TestCommentMentionGetBatch_ReturnsWrittenOrder(t *testing.T) {
	tx := beginRollbackTx(t)
	ctx := context.Background()
	postID := insertTestPost(t, tx)
	var commentID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO post.comments (post_id, author_id, content) VALUES ($1, $2, 'c') RETURNING id`,
		postID, uuid.New()).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	written := descendingUUIDs(4)

	r := &CommentMentionRepository{queries: db.New(tx)}
	insertOutOfOrder(t, written, func(uid uuid.UUID, pos int32) error { return r.Insert(ctx, commentID, uid, pos) })

	got, err := r.GetBatch(ctx, []uuid.UUID{commentID})
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	assertOrder(t, "comment", written, got[commentID])
}

func beginRollbackTx(t *testing.T) pgx.Tx {
	t.Helper()
	pool := openTestPool(t)
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func insertTestPost(t *testing.T, tx pgx.Tx) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(context.Background(),
		`INSERT INTO post.posts (author_id, content) VALUES ($1, 'mention order') RETURNING id`,
		uuid.New()).Scan(&id); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	return id
}

// descendingUUIDs returns n ids sorted high to low, the opposite of the order a
// primary-key scan would produce.
func descendingUUIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(b[:], a[:]) })
	return ids
}

// insertOutOfOrder inserts written back to front, each at its written position.
func insertOutOfOrder(t *testing.T, written []uuid.UUID, insert func(uuid.UUID, int32) error) {
	t.Helper()
	for i := len(written) - 1; i >= 0; i-- {
		if err := insert(written[i], int32(i)); err != nil {
			t.Fatalf("insert mention %d: %v", i, err)
		}
	}
}

func assertOrder(t *testing.T, label string, want, got []uuid.UUID) {
	t.Helper()
	if !slices.Equal(want, got) {
		t.Errorf("%s: want mentions in written order %v, got %v", label, want, got)
	}
}
