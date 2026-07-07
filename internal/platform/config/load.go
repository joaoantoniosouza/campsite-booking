package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidConfig is wrapped by every error LoadFrom returns.
var ErrInvalidConfig = errors.New("invalid config")

const minSessionSecretLen = 32

// Load builds Config from the process environment.
func Load() (Config, error) {
	return LoadFrom(os.Getenv)
}

// LoadFrom builds Config using getenv as the environment source, applying
// defaults and aggregating every validation issue into a single error
// wrapping ErrInvalidConfig. No partial config is ever returned.
func LoadFrom(getenv func(string) string) (Config, error) {
	var errs []string

	env := Environment(orDefault(getenv("APP_ENV"), string(EnvDevelopment)))
	switch env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Sprintf("APP_ENV: must be one of development/staging/production, got %q", env))
	}

	port, err := parseIntDefault(getenv("HTTP_PORT"), 8080)
	if err != nil {
		errs = append(errs, fmt.Sprintf("HTTP_PORT: %v", err))
	} else if port < 1 || port > 65535 {
		errs = append(errs, fmt.Sprintf("HTTP_PORT: must be 1..65535, got %d", port))
	}

	readTimeout, err := parseDurationDefault(getenv("HTTP_READ_TIMEOUT"), 15*time.Second)
	if err != nil {
		errs = append(errs, fmt.Sprintf("HTTP_READ_TIMEOUT: %v", err))
	}
	writeTimeout, err := parseDurationDefault(getenv("HTTP_WRITE_TIMEOUT"), 15*time.Second)
	if err != nil {
		errs = append(errs, fmt.Sprintf("HTTP_WRITE_TIMEOUT: %v", err))
	}
	idleTimeout, err := parseDurationDefault(getenv("HTTP_IDLE_TIMEOUT"), 60*time.Second)
	if err != nil {
		errs = append(errs, fmt.Sprintf("HTTP_IDLE_TIMEOUT: %v", err))
	}
	shutdownTimeout, err := parseDurationDefault(getenv("HTTP_SHUTDOWN_TIMEOUT"), 10*time.Second)
	if err != nil {
		errs = append(errs, fmt.Sprintf("HTTP_SHUTDOWN_TIMEOUT: %v", err))
	}

	dbURL := getenv("DATABASE_URL")
	if dbURL == "" {
		errs = append(errs, "DATABASE_URL: required")
	} else if _, err := url.Parse(dbURL); err != nil {
		errs = append(errs, fmt.Sprintf("DATABASE_URL: %v", err))
	}

	secret := getenv("SESSION_SECRET")
	if secret == "" {
		errs = append(errs, "SESSION_SECRET: required")
	} else if len(secret) < minSessionSecretLen {
		errs = append(errs, fmt.Sprintf("SESSION_SECRET: must be at least %d characters", minSessionSecretLen))
	}

	level, err := parseLevelDefault(getenv("LOG_LEVEL"), slog.LevelInfo)
	if err != nil {
		errs = append(errs, fmt.Sprintf("LOG_LEVEL: %v", err))
	}

	defaultFormat := LogText
	if env == EnvStaging || env == EnvProduction {
		defaultFormat = LogJSON
	}
	format := LogFormat(orDefault(getenv("LOG_FORMAT"), string(defaultFormat)))
	switch format {
	case LogText, LogJSON:
	default:
		errs = append(errs, fmt.Sprintf("LOG_FORMAT: must be text or json, got %q", format))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("%w: %s", ErrInvalidConfig, strings.Join(errs, "; "))
	}

	return Config{
		Env: env,
		HTTP: HTTPConfig{
			Port:            port,
			ReadTimeout:     readTimeout,
			WriteTimeout:    writeTimeout,
			IdleTimeout:     idleTimeout,
			ShutdownTimeout: shutdownTimeout,
		},
		DatabaseURL:   dbURL,
		SessionSecret: secret,
		Log:           LogConfig{Level: level, Format: format},
	}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseIntDefault(v string, def int) (int, error) {
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", v)
	}
	return n, nil
}

func parseDurationDefault(v string, def time.Duration) (time.Duration, error) {
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", v)
	}
	return d, nil
}

func parseLevelDefault(v string, def slog.Level) (slog.Level, error) {
	if v == "" {
		return def, nil
	}
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("must be one of debug/info/warn/error, got %q", v)
	}
}
