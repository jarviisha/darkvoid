package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

type fakeAuthorRepo struct {
	active, any int // calls to each lookup
	users       []*entity.User
}

func (r *fakeAuthorRepo) GetUsersByIDs(context.Context, []uuid.UUID) ([]*entity.User, error) {
	r.active++
	return r.users, nil
}

func (r *fakeAuthorRepo) GetUsersByIDsAny(context.Context, []uuid.UUID) ([]*entity.User, error) {
	r.any++
	return r.users, nil
}

func TestAuthorDirectory_ProjectsUsers(t *testing.T) {
	avatar := "a.png"
	u := &entity.User{ID: uuid.New(), Username: "alice", DisplayName: "Alice", AvatarKey: &avatar}
	repo := &fakeAuthorRepo{users: []*entity.User{u}}

	got, err := NewAuthorDirectory(repo).GetAuthorsByIDs(context.Background(), []uuid.UUID{u.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := got[u.ID]
	if a == nil || a.ID != u.ID || a.Username != "alice" || a.DisplayName != "Alice" || a.AvatarKey != &avatar {
		t.Errorf("got %+v, want the user's four public fields", a)
	}
}

// Content stays attributed to a deactivated author; a notification does not
// name a deactivated actor. The two constructors are what keeps that apart.
func TestAuthorDirectory_ActiveOnlyUsesActiveLookup(t *testing.T) {
	id := uuid.New()
	repo := &fakeAuthorRepo{}

	_, _ = NewAuthorDirectory(repo).GetAuthorsByIDs(context.Background(), []uuid.UUID{id})
	_, _ = NewActiveAuthorDirectory(repo).GetAuthorsByIDs(context.Background(), []uuid.UUID{id})

	if repo.any != 1 || repo.active != 1 {
		t.Errorf("any=%v active=%v, want one lookup each", repo.any, repo.active)
	}
}

func TestAuthorDirectory_NoIDs_NoQuery(t *testing.T) {
	repo := &fakeAuthorRepo{}
	got, err := NewAuthorDirectory(repo).GetAuthorsByIDs(context.Background(), nil)
	if err != nil || got == nil || repo.any != 0 {
		t.Errorf("got %v, %v after %d lookups; want an empty map and no query", got, err, repo.any)
	}
}
