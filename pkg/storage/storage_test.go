package storage

import "testing"

func TestNew_RejectsUnsupportedProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
	}{
		{name: "s3 not implemented", provider: "s3"},
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
