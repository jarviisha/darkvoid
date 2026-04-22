package service

import (
	"testing"

	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
)

// --------------------------------------------------------------------------
// Validation tests
// --------------------------------------------------------------------------

func TestValidateTags(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		want    []string
		wantErr bool
	}{
		{
			name: "valid tags",
			tags: []string{"golang", "webdev", "programming"},
			want: []string{"golang", "webdev", "programming"},
		},
		{
			name: "deduplication",
			tags: []string{"golang", "Golang", "GOLANG"},
			want: []string{"golang"},
		},
		{
			name: "whitespace trimming",
			tags: []string{"  golang  ", " webdev"},
			want: []string{"golang", "webdev"},
		},
		{
			name: "empty strings ignored",
			tags: []string{"golang", "", "  ", "webdev"},
			want: []string{"golang", "webdev"},
		},
		{
			name:    "max tags exceeded",
			tags:    []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"},
			wantErr: true,
		},
		{
			name:    "invalid characters - space",
			tags:    []string{"hello world"},
			wantErr: true,
		},
		{
			name:    "invalid characters - special",
			tags:    []string{"hello@world"},
			wantErr: true,
		},
		{
			name:    "too long",
			tags:    []string{"a123456789012345678901234567890123456789012345678901"},
			wantErr: true,
		},
		{
			name: "underscore allowed",
			tags: []string{"hello_world"},
			want: []string{"hello_world"},
		},
		{
			name: "numbers allowed",
			tags: []string{"golang123", "web2dev"},
			want: []string{"golang123", "web2dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateTags(tt.tags)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("validateTags() got %d tags, want %d", len(got), len(tt.want))
				return
			}
			for i, tag := range got {
				if tag != tt.want[i] {
					t.Errorf("validateTags()[%d] = %q, want %q", i, tag, tt.want[i])
				}
			}
		})
	}
}

func TestIsValidVisibility(t *testing.T) {
	tests := []struct {
		name       string
		visibility entity.Visibility
		want       bool
	}{
		{"public", entity.VisibilityPublic, true},
		{"followers", entity.VisibilityFollowers, true},
		{"private", entity.VisibilityPrivate, true},
		{"invalid", "invalid", false},
		{"empty", "", false},
		{"mixed case", "Public", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidVisibility(tt.visibility)
			if got != tt.want {
				t.Errorf("isValidVisibility(%q) = %v, want %v", tt.visibility, got, tt.want)
			}
		})
	}
}

func TestInferMediaType(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		// Video formats
		{"mp4 lowercase", "video.mp4", "video"},
		{"mp4 uppercase", "VIDEO.MP4", "video"},
		{"webm", "clip.webm", "video"},
		{"mov", "movie.mov", "video"},
		{"avi", "old.avi", "video"},
		{"mkv", "hd.mkv", "video"},
		{"m4v", "apple.m4v", "video"},
		{"flv", "flash.flv", "video"},
		{"wmv", "windows.wmv", "video"},
		{"ogv", "open.ogv", "video"},

		// Image formats
		{"jpg", "photo.jpg", "image"},
		{"jpeg", "image.jpeg", "image"},
		{"png", "graphic.png", "image"},
		{"gif", "animation.gif", "image"},
		{"webp", "modern.webp", "image"},
		{"bmp", "bitmap.bmp", "image"},
		{"svg", "vector.svg", "image"},
		{"heic", "iphone.heic", "image"},

		// Edge cases
		{"no extension", "no-extension", "image"},
		{"multiple dots", "file.backup.jpg", "image"},
		{"uppercase extension", "PHOTO.PNG", "image"},
		{"mixed case", "Video.Mp4", "video"},
		{"path with slashes", "uploads/media/video.mp4", "video"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferMediaType(tt.key)
			if got != tt.expected {
				t.Errorf("inferMediaType(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}
