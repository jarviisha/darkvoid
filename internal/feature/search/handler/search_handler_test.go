package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jarviisha/darkvoid/internal/feature/search/dto"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

type searchServiceStub struct {
	response      *dto.SearchResponse
	err           error
	query         string
	searchType    dto.SearchType
	limit, offset int32
}

func (s *searchServiceStub) Search(_ context.Context, query string, searchType dto.SearchType, limit, offset int32) (*dto.SearchResponse, error) {
	s.query, s.searchType, s.limit, s.offset = query, searchType, limit, offset
	return s.response, s.err
}

func TestSearch_ParsesParametersAndWritesResponse(t *testing.T) {
	t.Parallel()
	stub := &searchServiceStub{response: &dto.SearchResponse{Query: "go", Type: "posts"}}
	handler := NewSearchHandler(stub)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/search?q=go&type=posts&limit=12&offset=4", nil)

	handler.Search(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if stub.query != "go" || stub.searchType != dto.SearchTypePosts || stub.limit != 12 || stub.offset != 4 {
		t.Fatalf("service args = (%q, %q, %d, %d)", stub.query, stub.searchType, stub.limit, stub.offset)
	}
	var response dto.SearchResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Query != "go" || response.Type != "posts" {
		t.Fatalf("response = %#v", response)
	}
}

func TestSearch_InvalidParametersUseSafeDefaults(t *testing.T) {
	t.Parallel()
	stub := &searchServiceStub{response: &dto.SearchResponse{}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/search?q=go&limit=-1&offset=bad", nil)

	NewSearchHandler(stub).Search(recorder, request)

	if stub.searchType != dto.SearchTypeAll || stub.limit != 20 || stub.offset != 0 {
		t.Fatalf("service args = type %q limit %d offset %d", stub.searchType, stub.limit, stub.offset)
	}
}

func TestSearch_WritesServiceError(t *testing.T) {
	t.Parallel()
	stub := &searchServiceStub{err: pkgerrors.NewBadRequestError("invalid search")}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/search?q=x", nil)

	NewSearchHandler(stub).Search(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

type routeRecorder struct {
	pattern string
	handler http.HandlerFunc
}

func (r *routeRecorder) Get(pattern string, handler http.HandlerFunc) {
	r.pattern, r.handler = pattern, handler
}

func TestRegisterRoutes(t *testing.T) {
	t.Parallel()
	routes := &routeRecorder{}
	NewSearchHandler(&searchServiceStub{}).RegisterRoutes(routes)
	if routes.pattern != "/search" || routes.handler == nil {
		t.Fatalf("registered route = %q, %v", routes.pattern, routes.handler)
	}
}
