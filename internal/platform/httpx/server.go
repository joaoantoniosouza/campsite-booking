package httpx

import (
	"context"
	"net"
	"net/http"
	"time"
)

// Server wraps *http.Server with a Run method that serves until the given
// context is cancelled, then drains in-flight requests within a timeout.
type Server struct {
	Addr         string
	Handler      http.Handler
	DrainTimeout time.Duration

	// Started, if set, is called with the actual listener address once the
	// server starts accepting connections. Useful for tests binding to ":0".
	Started func(addr string)
}

// Run listens on s.Addr and serves s.Handler until ctx is cancelled. On
// cancellation it attempts a graceful shutdown, forcing closed connections
// after DrainTimeout elapses.
func (s *Server) Run(ctx context.Context) error {
	drainTimeout := s.DrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = 5 * time.Second
	}

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	if s.Started != nil {
		s.Started(ln.Addr().String())
	}

	srv := &http.Server{Handler: s.Handler}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			srv.Close()
		}
		return nil
	}
}
