package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/lumaya-wishlist-backend/internal/app"
	"github.com/lumaya-wishlist-backend/internal/database"
)

func main() {
	// Load local environment configuration.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found; using system environment variables")
	}

	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	)

	// Application shutdown context.
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// Read the existing PostgreSQL database connection string.
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	// Connect to the existing database.
	// This does not create or modify any database, role, or permission.
	databaseCtx, databaseCancel := context.WithTimeout(
		ctx,
		10*time.Second,
	)
	defer databaseCancel()

	pool, err := database.Connect(databaseCtx, databaseURL)
	if err != nil {
		logger.Error("PostgreSQL connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	logger.Info("PostgreSQL connection established")

	// Initialise the HTTP application.
	application, err := app.NewServer(pool)
	if err != nil {
		logger.Error("Application initialisation failed", "error", err)
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)

	go func() {
		logger.Info("Lumaya Waitlist API starting", "port", port)

		if err := httpServer.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		logger.Error("HTTP server failed", "error", err)
		stop()

	case <-ctx.Done():
		logger.Info("Shutdown signal received")
	}

	// Gracefully shut down the HTTP server.
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Graceful shutdown failed", "error", err)
	}

	logger.Info("Lumaya Waitlist API stopped")
}
