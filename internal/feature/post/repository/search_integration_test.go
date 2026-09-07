package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
)

// dsnEnv opts a run into the queries that need a real server. Every other test
// in this package runs against db.Querier, which proves the mapping but not the
// SQL: a query that does not parse, or whose arguments bind to the wrong
// placeholders, is invisible to a double and shows up only in production.
//
// The test skips rather than fails when the variable is unset, so `make test`
// is unaffected. Point it at a database that has migrations/post applied:
//
//	DARKVOID_TEST_DB_DSN='postgres://user:pass@localhost:5432/db?sslmode=disable' \
//	  go test ./internal/feature/post/repository/ -run SearchByQuery -v
//
// It only reads. Nothing here inserts, updates or truncates, so it is safe to
// aim at a development database.
const dsnEnv = "DARKVOID_TEST_DB_DSN"

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping the queries that need a server", dsnEnv)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

// searchTerm finds a word that the corpus actually indexes, so the assertions
// below are about paging rather than about whichever rows happen to exist.
func searchTerm(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	rows, err := pool.Query(context.Background(),
		`SELECT word FROM ts_stat(
		    $$SELECT search_vector FROM post.posts
		      WHERE deleted_at IS NULL AND visibility = 'public'$$)
		 WHERE ndoc >= 3 AND length(word) >= 4
		 ORDER BY ndoc DESC LIMIT 1`)
	if err != nil {
		t.Fatalf("find an indexed term: %v", err)
	}
	defer rows.Close()

	var word string
	for rows.Next() {
		if err := rows.Scan(&word); err != nil {
			t.Fatalf("scan term: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read terms: %v", err)
	}
	if word == "" {
		t.Skip("no indexed term appears in at least 3 public posts; nothing to page through")
	}
	return word
}

// The regenerated query binds LIMIT to $3 and OFFSET to $2 while sqlc passes
// the arguments as (query, offset, limit). Reading that as correct is not the
// same as running it: had the two been transposed, asking for one row at
// offset 2 would return two rows starting at offset 1, and every search page
// after the first would silently overlap the one before it.
func TestSearchByQuery_PagesThroughPostgres(t *testing.T) {
	pool := openTestPool(t)
	term := searchTerm(t, pool)
	r := NewPostSearchRepository(pool)
	ctx := context.Background()

	page, err := r.SearchByQuery(ctx, term, 3, 0)
	if err != nil {
		t.Fatalf("SearchByQuery(%q, 3, 0): %v", term, err)
	}
	if len(page) != 3 {
		t.Fatalf("want 3 results for %q, got %d", term, len(page))
	}

	third, err := r.SearchByQuery(ctx, term, 1, 2)
	if err != nil {
		t.Fatalf("SearchByQuery(%q, 1, 2): %v", term, err)
	}
	if len(third) != 1 {
		t.Fatalf("limit 1 offset 2 must return exactly 1 row, got %d — limit and offset are bound to the wrong placeholders", len(third))
	}
	if third[0].ID != page[2].ID {
		t.Errorf("limit 1 offset 2 must return the third row: want %v, got %v", page[2].ID, third[0].ID)
	}
}

// The WHERE clause and the projection are the other half of what a double
// cannot check: that the filter reaches the server, and that the nine columns
// still line up with the fields the mapper assigns.
func TestSearchByQuery_ReturnsMappedPublicPosts(t *testing.T) {
	pool := openTestPool(t)
	term := searchTerm(t, pool)
	r := NewPostSearchRepository(pool)

	posts, err := r.SearchByQuery(context.Background(), term, 10, 0)
	if err != nil {
		t.Fatalf("SearchByQuery: %v", err)
	}
	if len(posts) == 0 {
		t.Fatalf("want at least one result for %q", term)
	}
	for _, p := range posts {
		if p.Visibility != "public" {
			t.Errorf("post %v: visibility %q leaked past the filter", p.ID, p.Visibility)
		}
		if p.DeletedAt != nil {
			t.Errorf("post %v: soft-deleted row leaked past the filter", p.ID)
		}
		if p.ID.String() == "" || p.AuthorID.String() == "" {
			t.Errorf("post %v: ids not scanned", p.ID)
		}
		if p.CreatedAt.IsZero() {
			t.Errorf("post %v: CreatedAt not scanned — the column order may have shifted", p.ID)
		}
		if strings.TrimSpace(p.Content) == "" {
			t.Errorf("post %v: content not scanned", p.ID)
		}
	}
}

// diacriticProbe pulls a word out of the live corpus that changes when its
// marks are stripped, and returns it both ways. Taking the word from the corpus
// rather than hard-coding one keeps the test about the search path instead of
// about whether some fixture happens to be present.
func diacriticProbe(t *testing.T, pool *pgxpool.Pool) (written, typed string) {
	t.Helper()

	err := pool.QueryRow(context.Background(),
		`SELECT w.word, post.immutable_unaccent(w.word)
		   FROM post.posts p,
		        LATERAL (
		            SELECT unnest(regexp_split_to_array(p.content, '[^[:alpha:]]+')) AS word
		        ) w
		  WHERE p.deleted_at IS NULL
		    AND p.visibility = 'public'
		    AND length(w.word) >= 4
		    AND w.word <> post.immutable_unaccent(w.word)
		  LIMIT 1`).Scan(&written, &typed)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no public post contains an accented word; nothing to strip")
	}
	if err != nil {
		t.Fatalf("find an accented word: %v", err)
	}
	return written, typed
}

// Typing Vietnamese without diacritics is ordinary input, not an edge case.
// Under the 'english' configuration this replaced, 'tiền' and 'tien' were
// different lexemes, so the second search returned an empty page — which reads
// as "no results" rather than as a broken search, and is why the bug survived.
//
// The two searches must agree exactly, not merely both be non-empty: after
// unaccent both build the *same* tsquery, so any divergence means one half of
// the comparison stopped normalizing the way the other does. That is the
// failure this pins. It is deliberately end to end — the configuration is named
// in the generated column and again in the query, and reviewing either site
// alone cannot show that the two still match.
func TestSearchByQuery_MatchesWithoutDiacritics(t *testing.T) {
	pool := openTestPool(t)
	written, typed := diacriticProbe(t, pool)
	r := NewPostSearchRepository(pool)
	ctx := context.Background()

	asWritten, err := r.SearchByQuery(ctx, written, 50, 0)
	if err != nil {
		t.Fatalf("SearchByQuery(%q): %v", written, err)
	}
	if len(asWritten) == 0 {
		t.Fatalf("%q is a word from the corpus and must match at least the post it came from", written)
	}

	asTyped, err := r.SearchByQuery(ctx, typed, 50, 0)
	if err != nil {
		t.Fatalf("SearchByQuery(%q): %v", typed, err)
	}
	if len(asTyped) == 0 {
		t.Fatalf("searching %q found nothing while %q found %d — the query and the generated column no longer normalize the same way",
			typed, written, len(asWritten))
	}
	if !sameIDs(asWritten, asTyped) {
		t.Errorf("searching %q and %q must return the same rows in the same order: got %v and %v",
			written, typed, postIDs(asWritten), postIDs(asTyped))
	}
}

// stopwordProbe finds a word the corpus indexes that the 'english'
// configuration would have thrown away. Vietnamese is full of them — to (big),
// an (eat), do (because), no (it), so (compare), in (print) — and under
// 'english' none of them was searchable at all.
func stopwordProbe(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var word string
	err := pool.QueryRow(context.Background(),
		`SELECT w.word
		   FROM post.posts p,
		        LATERAL (
		            SELECT unnest(regexp_split_to_array(p.content, '[^[:alpha:]]+')) AS word
		        ) w
		  WHERE p.deleted_at IS NULL
		    AND p.visibility = 'public'
		    AND length(w.word) >= 2
		    AND to_tsvector('english', w.word) = ''::tsvector
		    AND to_tsvector('simple', post.immutable_unaccent(w.word)) <> ''::tsvector
		  LIMIT 1`).Scan(&word)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no post contains a word the english configuration would discard")
	}
	if err != nil {
		t.Fatalf("find a discarded word: %v", err)
	}
	return word
}

// The other half of the same bug: 'english' dropped these words from the index
// before they could be searched for, so the miss was in the writing path and no
// amount of fixing the query would have found them.
func TestSearchByQuery_FindsWordsEnglishWouldDiscard(t *testing.T) {
	pool := openTestPool(t)
	word := stopwordProbe(t, pool)

	posts, err := NewPostSearchRepository(pool).SearchByQuery(context.Background(), word, 10, 0)
	if err != nil {
		t.Fatalf("SearchByQuery(%q): %v", word, err)
	}
	if len(posts) == 0 {
		t.Errorf("%q appears in a public post but matches nothing; the english configuration indexes it as an empty vector", word)
	}
}

func postIDs(posts []*entity.Post) []uuid.UUID {
	ids := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	return ids
}

func sameIDs(a, b []*entity.Post) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}
