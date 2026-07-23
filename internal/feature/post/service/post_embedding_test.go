package service

import (
	"context"
	"testing"
	"time"
)

type capturedCatalogItem struct {
	objectID string
	content  string
	authorID string
}

type mockCatalogIngester struct {
	items chan capturedCatalogItem
}

func (m *mockCatalogIngester) IngestCatalogItem(_ context.Context, objectID, content, authorSubjectID string) error {
	m.items <- capturedCatalogItem{objectID: objectID, content: content, authorID: authorSubjectID}
	return nil
}

type mockObjectEmbedder struct {
	calls chan string
}

func (m *mockObjectEmbedder) UpsertObjectEmbedding(_ context.Context, objectID string, _ []float64, _ time.Time) error {
	m.calls <- objectID
	return nil
}

type mockEmbeddingProvider struct{}

func (mockEmbeddingProvider) Embed(_ context.Context, _ string) ([]float64, error) {
	return []float64{1, 0}, nil
}

func waitFor[T any](t *testing.T, ch chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

func TestPushEmbeddingAsync_CatalogMode_SendsContentTagsAndAuthor(t *testing.T) {
	ingester := &mockCatalogIngester{items: make(chan capturedCatalogItem, 1)}
	s := &PostService{}
	s.WithCatalogIngester(ingester)

	s.pushEmbeddingAsync("post-1", "hello world", []string{"go", "backend"}, "author-1", time.Now())

	got := waitFor(t, ingester.items, "catalog ingest")
	if got.objectID != "post-1" {
		t.Fatalf("objectID = %q, want post-1", got.objectID)
	}
	if got.content != "hello world go backend" {
		t.Fatalf("content = %q, want content joined with tags", got.content)
	}
	if got.authorID != "author-1" {
		t.Fatalf("authorID = %q, want author-1", got.authorID)
	}
}

func TestPushEmbeddingAsync_CatalogMode_OverridesBYOEPair(t *testing.T) {
	ingester := &mockCatalogIngester{items: make(chan capturedCatalogItem, 1)}
	embedder := &mockObjectEmbedder{calls: make(chan string, 1)}
	s := &PostService{}
	s.WithEmbedding(mockEmbeddingProvider{}, embedder)
	s.WithCatalogIngester(ingester)

	s.pushEmbeddingAsync("post-2", "content", nil, "author-2", time.Now())

	waitFor(t, ingester.items, "catalog ingest")
	select {
	case id := <-embedder.calls:
		t.Fatalf("BYOE embedder was called for %q, want catalog path only", id)
	default:
	}
}

func TestPushEmbeddingAsync_BYOEMode_PushesVector(t *testing.T) {
	embedder := &mockObjectEmbedder{calls: make(chan string, 1)}
	s := &PostService{}
	s.WithEmbedding(mockEmbeddingProvider{}, embedder)

	s.pushEmbeddingAsync("post-3", "content", nil, "author-3", time.Now())

	if id := waitFor(t, embedder.calls, "BYOE embedding upsert"); id != "post-3" {
		t.Fatalf("objectID = %q, want post-3", id)
	}
}

func TestPushEmbeddingAsync_NothingWired_NoOp(t *testing.T) {
	s := &PostService{}
	// Must not panic or spawn work when neither path is wired.
	s.pushEmbeddingAsync("post-4", "content", nil, "author-4", time.Now())
}
