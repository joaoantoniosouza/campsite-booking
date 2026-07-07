package httpx

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestServer_GracefulDrain(t *testing.T) {
	srv := &Server{
		Addr:    ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean shutdown, got error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

func TestServer_InFlightRequestCompletesDuringDrain(t *testing.T) {
	release := make(chan struct{})
	handlerDone := make(chan struct{})

	srv := &Server{
		Addr:         ":0",
		DrainTimeout: 2 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-release
			w.WriteHeader(http.StatusOK)
			close(handlerDone)
		}),
	}

	addrCh := make(chan string, 1)
	srv.Started = func(addr string) { addrCh <- addr }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	addr := <-addrCh

	reqDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
		}
		reqDone <- err
	}()

	time.Sleep(50 * time.Millisecond) // let the request reach the handler
	cancel()                          // trigger drain while request is in-flight
	close(release)                    // allow the handler to finish

	select {
	case err := <-reqDone:
		if err != nil {
			t.Fatalf("expected in-flight request to complete, got error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight request did not complete during drain")
	}

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not complete")
	}

	<-done
}

func TestServer_DrainTimeoutForceCloses(t *testing.T) {
	srv := &Server{
		Addr:         ":0",
		DrainTimeout: 50 * time.Millisecond,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(1 * time.Second) // never finishes before drain timeout
		}),
	}

	addrCh := make(chan string, 1)
	srv.Started = func(addr string) { addrCh <- addr }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	addr := <-addrCh

	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
		}
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected Run to still return cleanly after force-close, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not force-close within expected time")
	}
}
