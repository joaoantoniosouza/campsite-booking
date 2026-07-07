package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/campsite-booking/campsite-booking/internal/platform/bootstrap"
)

func main() {
	app, err := bootstrap.Bootstrap(os.Getenv, nil)
	if err != nil {
		slog.Default().Error("build failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		slog.Default().Error("server error", "error", err)
		os.Exit(1)
	}
}
