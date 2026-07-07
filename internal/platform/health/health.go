// Package health provides the liveness/readiness HTTP probe.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger is the consumer-owned port health depends on to check readiness
// (e.g. *pgxpool.Pool satisfies it with no adapter).
type Pinger interface {
	Ping(ctx context.Context) error
}

const pingTimeout = 3 * time.Second

// Handler returns an http.HandlerFunc for GET /healthz: 200 with
// {status:ok,db:ok} when p.Ping succeeds within a bounded timeout, 503 with
// {status:degraded,db:down} otherwise. It never panics.
func Handler(p Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")

		if err := p.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "degraded", "db": "down"})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "db": "ok"})
	}
}
