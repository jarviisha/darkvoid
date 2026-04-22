package service

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// tagNameRegex validates an individual tag name (alphanumeric + underscore, 1–50 chars).
var tagNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{1,50}$`)

// Media type extension mappings
var (
	videoExtensions = map[string]bool{
		".mp4":  true,
		".webm": true,
		".mov":  true,
		".avi":  true,
		".mkv":  true,
		".m4v":  true,
		".flv":  true,
		".wmv":  true,
		".ogv":  true,
	}

	imageExtensions = map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".bmp":  true,
		".svg":  true,
		".heic": true,
	}
)

// validateTags normalizes and validates an explicit tag list from the client.
// Returns a deduplicated, lowercase slice or an error if any tag is invalid.
func validateTags(tags []string) ([]string, error) {
	if len(tags) > 10 {
		return nil, errors.NewBadRequestError("max 10 tags per post")
	}

	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))

	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if !tagNameRegex.MatchString(t) {
			return nil, errors.NewBadRequestError("invalid tag: " + t)
		}
		if _, dup := seen[t]; !dup {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out, nil
}

// isValidVisibility checks if the visibility enum is valid.
func isValidVisibility(v entity.Visibility) bool {
	return v == entity.VisibilityPublic ||
		v == entity.VisibilityFollowers ||
		v == entity.VisibilityPrivate
}

// inferMediaType determines media type from file extension.
// Returns "video" for video files, "image" for image files.
// Defaults to "image" for backward compatibility with unknown extensions.
func inferMediaType(key string) string {
	ext := strings.ToLower(filepath.Ext(key))

	if videoExtensions[ext] {
		return "video"
	}
	if imageExtensions[ext] {
		return "image"
	}

	// Default to image for backward compatibility
	return "image"
}
