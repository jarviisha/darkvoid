package storage

import (
	"context"
	"testing"
)

func TestNew_RejectsUnsupportedProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
	}{
		{name: "unknown", provider: "unknown"},
		{name: "empty", provider: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := New(Config{Provider: tt.provider, BaseURL: "https://cdn.test"})
			if err == nil {
				t.Fatalf("New() store = %T, want error", store)
			}
			if store != nil {
				t.Fatalf("New() store = %T, want nil", store)
			}
		})
	}
}

func TestNewS3_SelectsSharedObjectStorage(t *testing.T) {
	store, err := NewWithContext(context.Background(), Config{
		Provider: "s3",
		BaseURL:  "https://cdn.example.com/media",
		S3: S3Config{
			Region:          "us-east-1",
			Bucket:          "darkvoid-media",
			AccessKeyID:     "test-access",
			SecretAccessKey: "test-secret",
		},
	})
	if err != nil {
		t.Fatalf("NewWithContext: %v", err)
	}
	if _, ok := store.(*s3Storage); !ok {
		t.Fatalf("store = %T, want *s3Storage", store)
	}
}
