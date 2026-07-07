package config

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/campsite",
		"SESSION_SECRET": strings.Repeat("s", 32),
	}
}

func getenvFrom(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestLoadFrom_ValidWithDefaults(t *testing.T) {
	cfg, err := LoadFrom(getenvFrom(validEnv()))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Env != EnvDevelopment {
		t.Errorf("expected default Env=development, got %q", cfg.Env)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadTimeout != 15*time.Second {
		t.Errorf("expected default read timeout 15s, got %s", cfg.HTTP.ReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != 15*time.Second {
		t.Errorf("expected default write timeout 15s, got %s", cfg.HTTP.WriteTimeout)
	}
	if cfg.HTTP.IdleTimeout != 60*time.Second {
		t.Errorf("expected default idle timeout 60s, got %s", cfg.HTTP.IdleTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected default shutdown timeout 10s, got %s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Log.Level != slog.LevelInfo {
		t.Errorf("expected default log level info, got %v", cfg.Log.Level)
	}
	if cfg.Log.Format != LogText {
		t.Errorf("expected default log format text in dev, got %q", cfg.Log.Format)
	}
}

func TestLoadFrom_MissingDatabaseURL(t *testing.T) {
	env := validEnv()
	delete(env, "DATABASE_URL")

	_, err := LoadFrom(getenvFrom(env))
	if err == nil {
		t.Fatal("expected an error for missing DATABASE_URL")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected error to wrap ErrInvalidConfig, got: %v", err)
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("expected error to name DATABASE_URL, got: %v", err)
	}
}

func TestLoadFrom_MissingSessionSecret(t *testing.T) {
	env := validEnv()
	delete(env, "SESSION_SECRET")

	_, err := LoadFrom(getenvFrom(env))
	if err == nil {
		t.Fatal("expected an error for missing SESSION_SECRET")
	}
	if !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Errorf("expected error to name SESSION_SECRET, got: %v", err)
	}
}

func TestLoadFrom_AggregatesBothMissing(t *testing.T) {
	_, err := LoadFrom(getenvFrom(map[string]string{}))
	if err == nil {
		t.Fatal("expected an error for an empty environment")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Errorf("expected aggregated error naming both DATABASE_URL and SESSION_SECRET, got: %v", err)
	}
}

func TestLoadFrom_ShortSessionSecret(t *testing.T) {
	env := validEnv()
	env["SESSION_SECRET"] = "too-short"

	_, err := LoadFrom(getenvFrom(env))
	if err == nil {
		t.Fatal("expected an error for a too-short SESSION_SECRET")
	}
	if !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Errorf("expected error to name SESSION_SECRET, got: %v", err)
	}
}

func TestLoadFrom_BadHTTPPort(t *testing.T) {
	env := validEnv()
	env["HTTP_PORT"] = "not-a-number"

	_, err := LoadFrom(getenvFrom(env))
	if err == nil {
		t.Fatal("expected an error for an invalid HTTP_PORT")
	}
	if !strings.Contains(err.Error(), "HTTP_PORT") {
		t.Errorf("expected error to name HTTP_PORT, got: %v", err)
	}
}

func TestLoadFrom_UnknownLogLevel(t *testing.T) {
	env := validEnv()
	env["LOG_LEVEL"] = "verbose"

	_, err := LoadFrom(getenvFrom(env))
	if err == nil {
		t.Fatal("expected an error for an unknown LOG_LEVEL")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Errorf("expected error to name LOG_LEVEL, got: %v", err)
	}
}

func TestLoadFrom_UnknownLogFormat(t *testing.T) {
	env := validEnv()
	env["LOG_FORMAT"] = "xml"

	_, err := LoadFrom(getenvFrom(env))
	if err == nil {
		t.Fatal("expected an error for an unknown LOG_FORMAT")
	}
	if !strings.Contains(err.Error(), "LOG_FORMAT") {
		t.Errorf("expected error to name LOG_FORMAT, got: %v", err)
	}
}
