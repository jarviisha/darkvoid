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

func TestIngestCatalogAsync_SendsContentTagsAndAuthor(t *testing.T) {
	ingester := &mockCatalogIngester{items: make(chan capturedCatalogItem, 1)}
	s := &PostService{}
	s.WithCatalogIngester(ingester)

	s.ingestCatalogAsync("post-1", "hello world", []string{"go", "backend"}, "author-1")

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

func TestIngestCatalogAsync_NothingWired_NoOp(t *testing.T) {
	s := &PostService{}
	// Must not panic or spawn work when no ingester is wired.
	s.ingestCatalogAsync("post-4", "content", nil, "author-4")
}

// IndexText is exported so the reindex command in cmd/darkvoidctl produces byte
// identical text to the live ingest path. These pin the shape both rely on: a
// backfill that joined content and tags differently would index old posts under
// a different representation than new ones, and nothing would ever report it.
func TestIndexText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		tags    []string
		want    string
	}{
		{"no tags is the content alone", "cà phê sáng", nil, "cà phê sáng"},
		{"empty tag slice is not a trailing space", "cà phê sáng", []string{}, "cà phê sáng"},
		{"tags are appended space separated", "hello world", []string{"go", "backend"}, "hello world go backend"},
		{"one tag", "hello", []string{"go"}, "hello go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IndexText(tt.content, tt.tags); got != tt.want {
				t.Errorf("IndexText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The live path must go through IndexText, or exporting it achieves nothing.
func TestIngestCatalogAsync_UsesIndexText(t *testing.T) {
	ingester := &mockCatalogIngester{items: make(chan capturedCatalogItem, 1)}
	s := &PostService{}
	s.WithCatalogIngester(ingester)

	content, tags := "bún chả Hà Nội", []string{"amthuc", "hanoi"}
	s.ingestCatalogAsync("post-9", content, tags, "author-9")

	got := waitFor(t, ingester.items, "catalog ingest")
	if want := IndexText(content, tags); got.content != want {
		t.Errorf("content = %q, want IndexText output %q", got.content, want)
	}
}
