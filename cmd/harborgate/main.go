package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/m3yyyyy/harborgate-api-gateway-control-plane/internal/api"
	"github.com/m3yyyyy/harborgate-api-gateway-control-plane/internal/store"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Printf("harborgate %s (%s)\n", version, commit)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	port := envOr("PORT", "8080")
	dataPath := envOr("DATA_PATH", filepath.Join("data", "harborgate.db"))
	webDir := envOr("WEB_DIR", filepath.Join("web", "dist"))

	database, err := store.Open(dataPath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.Seed(); err != nil {
		logger.Error("seed database", "error", err)
		os.Exit(1)
	}

	handler := api.New(api.Config{
		Store:      database,
		AdminToken: os.Getenv("ADMIN_TOKEN"),
		WebDir:     webDir,
		Version:    version,
		Commit:     commit,
		Logger:     logger,
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("HarborGate listening", "address", server.Addr, "version", version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown", "error", err)
		os.Exit(1)
	}
	logger.Info("HarborGate stopped")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
