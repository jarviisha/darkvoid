package service

import (
	"strings"
	"testing"
)

// TestNewCommentLikeService_NamesEveryMissingDependency is the guard that
// replaces the nil checks scattered through the call paths. Those checks turned
// a forgotten dependency into a behaviour change — a followers-only comment
// answering 404 to the people entitled to read it — with no error and nothing
// above Debug in the log.
//
// It asserts every name rather than just that an error came back, because boot
// is a slow edit-retry loop: reporting one missing dependency at a time costs a
// restart per field.
func TestNewCommentLikeService_NamesEveryMissingDependency(t *testing.T) {
	_, err := NewCommentLikeService(CommentLikeDeps{})

	if err == nil {
		t.Fatal("NewCommentLikeService(CommentLikeDeps{}) returned a nil error — want the missing dependencies named")
	}
	for _, want := range []string{"CommentLikes", "Comments", "Posts", "FollowChecker", "Notifications"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the missing %s dependency", err, want)
		}
	}
}

// TestNewLikeService_NamesEveryMissingDependency covers the service where the
// required/optional split actually bites: the behaviour-event publisher is the
// one dependency a deployment legitimately runs without, since it only exists
// when CODOHUE_ENABLED is set.
func TestNewLikeService_NamesEveryMissingDependency(t *testing.T) {
	_, err := NewLikeService(LikeDeps{})

	if err == nil {
		t.Fatal("NewLikeService(LikeDeps{}) returned a nil error — want the missing dependencies named")
	}
	for _, want := range []string{"Likes", "Posts", "FollowChecker", "Notifications"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the missing %s dependency", err, want)
		}
	}
	if strings.Contains(err.Error(), "BehaviorEvents") {
		t.Errorf("error %q names BehaviorEvents — it is optional and absent on every deployment with Codohue off", err)
	}
}

// TestNewCommentService_NamesEveryMissingDependency also pins a reclassification.
// CommentLikes and CommentMentions were functional options documented as
// optional, but SetupPostContext has always passed both, so "optional" only ever
// described the test doubles. They are required here, and the Codohue publisher
// is the sole survivor of the optional set.
func TestNewCommentService_NamesEveryMissingDependency(t *testing.T) {
	_, err := NewCommentService(CommentDeps{})

	if err == nil {
		t.Fatal("NewCommentService(CommentDeps{}) returned a nil error — want the missing dependencies named")
	}
	for _, want := range []string{
		"Pool", "Comments", "CommentMedia", "Posts", "Users",
		"CommentLikes", "CommentMentions", "FollowChecker", "Notifications",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the missing %s dependency", err, want)
		}
	}
	if strings.Contains(err.Error(), "BehaviorEvents") {
		t.Errorf("error %q names BehaviorEvents — it is optional and absent when Codohue is off", err)
	}
}

// TestNewPostService_NamesEveryMissingDependency covers the constructor whose
// silence had the sharpest edge. A forgotten FollowChecker made
// requirePostVisibility answer ErrPostNotFound for every followers-only post,
// which reaches the caller as a 404 indistinguishable from a deleted post — for
// exactly the followers entitled to read it.
//
// TrendingInvalidator and FeedOutbox move into the required set with it: both
// are wired on every deployment, so their nil branches described a configuration
// that has never existed.
func TestNewPostService_NamesEveryMissingDependency(t *testing.T) {
	_, err := NewPostService(PostDeps{})

	if err == nil {
		t.Fatal("NewPostService(PostDeps{}) returned a nil error — want the missing dependencies named")
	}
	for _, want := range []string{
		"Pool", "Posts", "Media", "Users", "Hashtags", "Likes", "Mentions",
		"FollowChecker", "Notifications", "TrendingInvalidator", "FeedOutbox",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the missing %s dependency", err, want)
		}
	}
	for _, codohue := range []string{"ObjectDeleter", "CatalogIngester"} {
		if strings.Contains(err.Error(), codohue) {
			t.Errorf("error %q names %s — it is optional and absent when Codohue is off", err, codohue)
		}
	}
}
