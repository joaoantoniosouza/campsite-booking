package log

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type statusAndBytesWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusAndBytesWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusAndBytesWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Middleware injects a request-scoped logger into the request context and
// emits exactly one structured record per completed request: method, path,
// status, bytes, duration_ms, and request_id when chi's RequestID middleware
// set one.
func Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusAndBytesWriter{ResponseWriter: w, status: http.StatusOK}
			ctx := IntoContext(r.Context(), logger)
			start := time.Now()

			next.ServeHTTP(sw, r.WithContext(ctx))

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"bytes", sw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if reqID := middleware.GetReqID(r.Context()); reqID != "" {
				attrs = append(attrs, "request_id", reqID)
			}
			logger.Info("request", attrs...)
		})
	}
}
