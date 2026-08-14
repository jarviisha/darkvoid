package service

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"

	"github.com/jarviisha/darkvoid/internal/feature/search/dto"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

type userSearcherStub struct {
	mu            sync.Mutex
	results       []dto.UserResult
	err           error
	query         string
	limit, offset int32
}

func (s *userSearcherStub) SearchByQuery(_ context.Context, query string, limit, offset int32) ([]dto.UserResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.query, s.limit, s.offset = query, limit, offset
	return s.results, s.err
}

type postSearcherStub struct {
	mu            sync.Mutex
	results       []dto.PostResult
	err           error
	query         string
	limit, offset int32
}

func (s *postSearcherStub) SearchByQuery(_ context.Context, query string, limit, offset int32) ([]dto.PostResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.query, s.limit, s.offset = query, limit, offset
	return s.results, s.err
}

type hashtagSearcherStub struct {
	mu      sync.Mutex
	results []string
	err     error
	query   string
	limit   int32
}

func (s *hashtagSearcherStub) SearchByPrefix(_ context.Context, query string, limit int32) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.query, s.limit = query, limit
	return s.results, s.err
}

func TestSearch_ValidatesQuery(t *testing.T) {
	t.Parallel()
	svc := NewSearchService(&userSearcherStub{}, &postSearcherStub{}, &hashtagSearcherStub{})
	for _, query := range []string{"", "x"} {
		_, err := svc.Search(context.Background(), query, dto.SearchTypeAll, 20, 0)
		appErr := pkgerrors.GetAppError(err)
		if appErr == nil || appErr.HTTPStatus != 400 {
			t.Fatalf("Search(%q) error = %v, want bad request", query, err)
		}
	}
}

func TestSearch_FocusedTypesApplyBoundsAndPropagateFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		searchType dto.SearchType
		limit      int32
		wantLimit  int32
		fail       bool
	}{
		{name: "users default limit", searchType: dto.SearchTypeUsers, limit: 0, wantLimit: defaultLimit},
		{name: "posts maximum limit", searchType: dto.SearchTypePosts, limit: 500, wantLimit: maxLimit},
		{name: "hashtags custom limit", searchType: dto.SearchTypeHashtags, limit: 7, wantLimit: 7},
		{name: "focused failure", searchType: dto.SearchTypeUsers, limit: 5, wantLimit: 5, fail: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			users := &userSearcherStub{results: []dto.UserResult{{ID: "user-1"}}}
			posts := &postSearcherStub{results: []dto.PostResult{{ID: "post-1"}}}
			hashtags := &hashtagSearcherStub{results: []string{"golang"}}
			if tt.fail {
				users.err = stderrors.New("database unavailable")
			}
			svc := NewSearchService(users, posts, hashtags)
			resp, err := svc.Search(context.Background(), "go", tt.searchType, tt.limit, 3)
			if tt.fail {
				if appErr := pkgerrors.GetAppError(err); appErr == nil || appErr.HTTPStatus != 500 {
					t.Fatalf("Search() error = %v, want internal error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if resp.Query != "go" || resp.Type != string(tt.searchType) {
				t.Fatalf("response metadata = %#v", resp)
			}
			switch tt.searchType {
			case dto.SearchTypeUsers:
				if users.limit != tt.wantLimit || users.offset != 3 || len(resp.Users) != 1 {
					t.Fatalf("users call/response = limit %d offset %d users %v", users.limit, users.offset, resp.Users)
				}
			case dto.SearchTypePosts:
				if posts.limit != tt.wantLimit || posts.offset != 3 || len(resp.Posts) != 1 {
					t.Fatalf("posts call/response = limit %d offset %d posts %v", posts.limit, posts.offset, resp.Posts)
				}
			case dto.SearchTypeHashtags:
				if hashtags.limit != tt.wantLimit || len(resp.Hashtags) != 1 {
					t.Fatalf("hashtags call/response = limit %d hashtags %v", hashtags.limit, resp.Hashtags)
				}
			}
		})
	}
}

func TestSearch_AllModeRunsEverySourceAndDegradesGracefully(t *testing.T) {
	t.Parallel()
	users := &userSearcherStub{results: []dto.UserResult{{ID: "user-1"}}}
	posts := &postSearcherStub{err: stderrors.New("posts unavailable")}
	hashtags := &hashtagSearcherStub{results: []string{"golang"}}

	resp, err := NewSearchService(users, posts, hashtags).Search(context.Background(), "go", dto.SearchTypeAll, 50, 99)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(resp.Users) != 1 || len(resp.Posts) != 0 || len(resp.Hashtags) != 1 {
		t.Fatalf("degraded response = %#v", resp)
	}
	if users.limit != allModeLimit || users.offset != 0 || posts.limit != allModeLimit || hashtags.limit != allModeLimit {
		t.Fatalf("all-mode calls = users(%d,%d) posts(%d,%d) hashtags(%d)", users.limit, users.offset, posts.limit, posts.offset, hashtags.limit)
	}
}
