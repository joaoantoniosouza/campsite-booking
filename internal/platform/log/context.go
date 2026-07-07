package log

import (
	"context"
	"log/slog"
)

type loggerContextKey struct{}

// IntoContext returns a copy of ctx carrying l as the request-scoped logger.
func IntoContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, l)
}

// FromContext returns the logger placed by IntoContext, or slog.Default()
// if none was injected.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
