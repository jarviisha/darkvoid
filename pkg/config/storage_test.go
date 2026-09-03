package config

import (
	"strings"
	"testing"
)

func validProductionS3Config() *Config {
	cfg := validCookieConfig()
	cfg.App.Environment = "production"
	cfg.Storage = StorageConfig{
		Provider: "s3",
		BaseURL:  "https://cdn.example.com/media",
		S3: S3StorageConfig{
			Region: "us-east-1",
			Bucket: "darkvoid-media",
		},
	}
	return cfg
}

func TestValidate_ProductionRequiresSharedObjectStorage(t *testing.T) {
	cfg := validProductionS3Config()
	cfg.Storage = StorageConfig{Provider: "local", BaseURL: "https://api.example.com/static", LocalDir: "/app/uploads"}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "production") || !strings.Contains(err.Error(), "s3") {
		t.Fatalf("Validate() error = %v, want production shared-storage error", err)
	}
}

func TestValidate_ProductionRequiresPublicHTTPSStorageURL(t *testing.T) {
	for _, baseURL := range []string{"", "http://cdn.example.com/media", "https://localhost:9000/media", "https://127.0.0.1/media"} {
		t.Run(baseURL, func(t *testing.T) {
			cfg := validProductionS3Config()
			cfg.Storage.BaseURL = baseURL
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() = nil for production storage URL %q", baseURL)
			}
		})
	}
}

func TestValidate_S3RequiresRegionBucketAndCredentialPair(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*S3StorageConfig)
	}{
		{name: "region", mutate: func(c *S3StorageConfig) { c.Region = "" }},
		{name: "bucket", mutate: func(c *S3StorageConfig) { c.Bucket = "" }},
		{name: "secret without access key", mutate: func(c *S3StorageConfig) { c.SecretAccessKey = "secret" }},
		{name: "access key without secret", mutate: func(c *S3StorageConfig) { c.AccessKeyID = "access" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validProductionS3Config()
			tt.mutate(&cfg.Storage.S3)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() = nil for invalid %s config", tt.name)
			}
		})
	}
}

func TestValidate_AllowsS3DefaultCredentialChain(t *testing.T) {
	if err := validProductionS3Config().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want IAM/default credential chain to be allowed", err)
	}
}

func TestLoadStorageConfig_LoadsS3CompatibleSettings(t *testing.T) {
	t.Setenv("STORAGE_PROVIDER", " S3 ")
	t.Setenv("STORAGE_BASE_URL", " https://cdn.example.com/media ")
	t.Setenv("STORAGE_S3_ENDPOINT", " http://minio:9000 ")
	t.Setenv("STORAGE_S3_REGION", "us-east-1")
	t.Setenv("STORAGE_S3_BUCKET", "darkvoid-media")
	t.Setenv("STORAGE_S3_ACCESS_KEY_ID", "access")
	t.Setenv("STORAGE_S3_SECRET_ACCESS_KEY", "secret")
	t.Setenv("STORAGE_S3_USE_PATH_STYLE", "true")

	cfg := loadStorageConfig()
	if cfg.Provider != "s3" || cfg.BaseURL != "https://cdn.example.com/media" {
		t.Fatalf("provider/base URL = %q/%q", cfg.Provider, cfg.BaseURL)
	}
	if cfg.S3.Endpoint != "http://minio:9000" || cfg.S3.Bucket != "darkvoid-media" || !cfg.S3.UsePathStyle {
		t.Fatalf("S3 config = %+v", cfg.S3)
	}
}
