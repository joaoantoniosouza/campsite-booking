// Package log builds the application slog.Logger and its request-logging
// middleware.
package log

import (
	"io"
	"log/slog"
	"os"

	"github.com/campsite-booking/campsite-booking/internal/platform/config"
)

// New builds a *slog.Logger writing to stderr at cfg.Level, using a JSON or
// text handler per cfg.Format.
func New(cfg config.LogConfig) *slog.Logger {
	return newWithWriter(os.Stderr, cfg)
}

func newWithWriter(w io.Writer, cfg config.LogConfig) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}

	var handler slog.Handler
	if cfg.Format == config.LogJSON {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	return slog.New(handler)
}
