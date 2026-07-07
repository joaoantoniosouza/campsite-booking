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
	logger := slog.Default()

	app, err := bootstrap.New(bootstrap.Deps{
		Addr:          ":8080",
		Logger:        logger,
		SessionSecret: []byte("dev-only-insecure-session-secret-change-me"),
	})
	if err != nil {
		logger.Error("build failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
