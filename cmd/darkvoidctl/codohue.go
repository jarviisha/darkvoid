package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	postrepo "github.com/jarviisha/darkvoid/internal/feature/post/repository"
	postsvc "github.com/jarviisha/darkvoid/internal/feature/post/service"
	"github.com/jarviisha/darkvoid/pkg/codohue"
	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/tfidf"
)

// reindexPageSize is how many posts are read per database round trip. Ingestion
// is one HTTP call per post either way, so this only trades round trips for
// memory; a page is small enough to hold comfortably.
const reindexPageSize = 100

func runCodohue(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}

	switch args[0] {
	case "reindex":
		if err := cmdReindex(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown codohue command %q\n\n", args[0])
		usage()
		os.Exit(2)
	}
}

// cmdReindex re-sends existing posts to Codohue's index.
//
// Indexing happens once, when a post is created, and is deliberately fire and
// forget — a failed ingest is logged and dropped rather than retried, so a post
// created while Codohue was unreachable is simply absent from recommendations
// forever. There is no queue and no repair pass; this command is the repair
// pass. Ingestion is an upsert, so re-sending a post that is already indexed
// costs a request and changes nothing.
func cmdReindex(args []string) error {
	fs := flag.NewFlagSet("codohue reindex", flag.ExitOnError)
	since := fs.Duration("since", 0, "only posts created within this window, e.g. 24h (0 = all posts)")
	limit := fs.Int("limit", 0, "stop after N posts (0 = no limit)")
	dryRun := fs.Bool("dry-run", false, "count what would be sent without sending it")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.Codohue.Enabled {
		return errors.New("CODOHUE_ENABLED is false — nothing to reindex into")
	}
	if cfg.Codohue.NamespaceKey == "" {
		return errors.New("CODOHUE_NAMESPACE_KEY is not set — the API would reject every call")
	}

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	posts := postrepo.NewPostRepository(d.pool)
	hashtags := postrepo.NewHashtagRepository(d.pool)

	// Redis is nil: this pushes index entries, never behavior events.
	client := codohue.NewClient(cfg.Codohue.BaseURL, cfg.Codohue.NamespaceKey, cfg.Codohue.Namespace, nil)
	if client == nil {
		return fmt.Errorf("could not build a codohue client for %q", cfg.Codohue.BaseURL)
	}

	// Fail before walking the corpus rather than after a few hundred failures.
	if !*dryRun {
		if err := client.Ping(ctx); err != nil {
			return fmt.Errorf("codohue unreachable at %s: %w", cfg.Codohue.BaseURL, err)
		}
	}

	var cutoff time.Time
	if *since > 0 {
		cutoff = time.Now().Add(-*since)
	}

	fmt.Printf("reindexing into namespace %q at %s (dense_source=%s)\n",
		cfg.Codohue.Namespace, cfg.Codohue.BaseURL, cfg.Codohue.DenseSource)
	if *dryRun {
		fmt.Println("dry run: nothing will be sent")
	}

	r := &reindexer{
		client:      client,
		denseSource: cfg.Codohue.DenseSource,
		vectorizer:  tfidf.New(cfg.Codohue.EmbeddingDim),
		dryRun:      *dryRun,
	}

	// Newest first, the same cursor walk the discover feed uses, so it inherits
	// that query's filters: public posts only, deleted ones excluded.
	var (
		cursorTime pgtype.Timestamptz
		cursorID   uuid.UUID
		sent, fail int
	)
	for {
		page, err := posts.GetDiscoverWithCursor(ctx, cursorTime, cursorID, reindexPageSize)
		if err != nil {
			return fmt.Errorf("read posts: %w", err)
		}
		if len(page) == 0 {
			break
		}

		ids := make([]uuid.UUID, len(page))
		for i, p := range page {
			ids[i] = p.ID
		}
		tagsByPost, err := hashtags.GetNamesByPostIDs(ctx, ids)
		if err != nil {
			// Indexing without tags would store a different text than the live
			// path does. Stop rather than silently degrade the whole backfill.
			return fmt.Errorf("read hashtags: %w", err)
		}

		reachedCutoff := false
		for _, p := range page {
			if !cutoff.IsZero() && p.CreatedAt.Before(cutoff) {
				reachedCutoff = true
				break
			}

			if err := r.send(ctx, p.ID.String(), p.Content, tagsByPost[p.ID], p.AuthorID.String(), p.CreatedAt); err != nil {
				if errors.Is(err, codohue.ErrUnavailable) {
					// The breaker opened: Codohue went down mid-run. Everything
					// after this would "fail" instantly without being attempted,
					// so report honestly instead of burning through the corpus.
					return fmt.Errorf("codohue became unavailable after %d posts (%d failed); rerun when it is back", sent, fail)
				}
				fmt.Fprintf(os.Stderr, "  post %s: %v\n", p.ID, err)
				fail++
				continue
			}
			sent++

			if *limit > 0 && sent >= *limit {
				fmt.Printf("done: %d posts sent, %d failed (stopped at -limit)\n", sent, fail)
				return nil
			}
			if sent%reindexPageSize == 0 {
				fmt.Printf("  %d posts...\n", sent)
			}
		}

		if reachedCutoff {
			break
		}
		last := page[len(page)-1]
		cursorTime = pgtype.Timestamptz{Time: last.CreatedAt, Valid: true}
		cursorID = last.ID
	}

	fmt.Printf("done: %d posts sent, %d failed\n", sent, fail)
	if fail > 0 {
		return fmt.Errorf("%d posts could not be indexed", fail)
	}
	return nil
}

// reindexer sends one post through whichever dense path this deployment uses.
type reindexer struct {
	client      *codohue.Client
	denseSource string
	vectorizer  *tfidf.Vectorizer
	dryRun      bool
}

func (r *reindexer) send(ctx context.Context, postID, content string, tags []string, authorID string, createdAt time.Time) error {
	// Shared with the live ingest path so a backfill cannot index posts under a
	// different text than new posts get.
	text := postsvc.IndexText(content, tags)

	if r.dryRun {
		return nil
	}

	if r.denseSource == codohue.DenseSourceCatalog {
		return r.client.IngestCatalogItem(ctx, postID, text, authorID)
	}

	vec, err := r.vectorizer.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("vectorize: %w", err)
	}
	return r.client.UpsertObjectEmbedding(ctx, postID, vec, createdAt)
}
