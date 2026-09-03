package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	t.Setenv("LOG_FORMAT", "TEXT")
	t.Setenv("LOG_ADD_SOURCE", "true")
	t.Setenv("SERVICE_NAME", "api")
	t.Setenv("SERVICE_VERSION", "v1")
	t.Setenv("ENVIRONMENT", "test")
	cfg := LoadConfigFromEnv()
	if cfg.Level != "debug" || cfg.Format != "text" || !cfg.AddSource || cfg.Service != "api" || cfg.Version != "v1" || cfg.Environment != "test" {
		t.Fatalf("config = %#v", cfg)
	}
	if Development().Environment != "development" || Production().Environment != "production" || Testing().Environment != "testing" {
		t.Fatal("environment presets are invalid")
	}
}

func TestLogger_StructuredHelpersAndContext(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	log := New(&Config{Level: "debug", Format: "json", Output: &output, Service: "api", Version: "v1", Environment: "test"})
	ctx := WithLogger(context.Background(), log)
	ctx = WithRequestID(ctx, "request-1")
	ctx = WithUserID(ctx, "user-1")
	ctx = WithFields(ctx, map[string]any{"scope": "test"})
	if got := WithFields(ctx, nil); got != ctx {
		t.Fatal("WithFields(nil) must preserve context")
	}
	Debug(ctx, "debug message")
	Info(ctx, "info message")
	Warn(ctx, "warn message")
	Error(ctx, "error message")
	LogError(ctx, errors.New("outer: inner"), "operation failed")
	log.WithGroup("database").LogDB("select", "users", 1.5)
	log.With("component", "auth").LogAuth("login", "user-1", true)
	if !strings.Contains(output.String(), `"request_id":"request-1"`) || !strings.Contains(output.String(), `"user_id":"user-1"`) || !strings.Contains(output.String(), `"scope":"test"`) {
		t.Fatalf("context fields missing from logs: %s", output.String())
	}
	before := output.Len()
	log.LogError(nil, "ignored")
	if output.Len() != before {
		t.Fatal("LogError(nil) wrote a log entry")
	}
}

func TestLogger_WithContextAndLevelParsing(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"debug", "info", "warn", "error", "unknown"} {
		var output bytes.Buffer
		log := New(&Config{Level: level, Format: "text", Output: &output})
		log.WithContext(context.Background()).Error("message")
		if !strings.Contains(output.String(), "message") {
			t.Fatalf("level %q output = %s", level, output.String())
		}
	}
	if New(nil) == nil || FromContext(context.Background()) == nil {
		t.Fatal("default logger is nil")
	}
}

func TestRecoveryMiddleware_HidesPanicAndLogsIt(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	log := New(&Config{Level: "error", Format: "json", Output: &output})
	handler := RecoveryMiddleware(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret panic")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic", nil))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &entry); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if entry["msg"] != "panic recovered" {
		t.Fatalf("log = %#v", entry)
	}
}
