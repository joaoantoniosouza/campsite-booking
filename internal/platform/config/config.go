package config

import (
	"log/slog"
	"time"
)

// Environment is the deployment environment the process is running in.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// LogFormat selects the slog handler used by the log package.
type LogFormat string

const (
	LogText LogFormat = "text"
	LogJSON LogFormat = "json"
)

// LogConfig configures the application logger.
type LogConfig struct {
	Level  slog.Level // LOG_LEVEL: debug|info|warn|error (default info)
	Format LogFormat  // LOG_FORMAT: text|json (default text in dev, else json)
}

// HTTPConfig configures the HTTP server.
type HTTPConfig struct {
	Port            int           // HTTP_PORT (default 8080, 1..65535)
	ReadTimeout     time.Duration // HTTP_READ_TIMEOUT (default 15s)
	WriteTimeout    time.Duration // HTTP_WRITE_TIMEOUT (default 15s)
	IdleTimeout     time.Duration // HTTP_IDLE_TIMEOUT (default 60s)
	ShutdownTimeout time.Duration // HTTP_SHUTDOWN_TIMEOUT (default 10s) — consumed by SKEL shutdown
}

// Config is the fully validated runtime configuration for the process.
type Config struct {
	Env           Environment // APP_ENV (default development)
	HTTP          HTTPConfig
	DatabaseURL   string // DATABASE_URL (required) — consumed by DATA pool
	SessionSecret string // SESSION_SECRET (required, >= 32 chars) — consumed by M1 auth
	Log           LogConfig
}
