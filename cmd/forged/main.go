package main

import (
	"context"
	"net/http"
	"os"

	"forge/internal/config"
	"forge/internal/db"
	"forge/internal/logger"
	"log/slog"
)

func main() {
	logger.Init()
	cfg := config.LoadConfig()

	ctx := context.Background()
	pool, err := db.Init(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to init db", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.ApplyMigrations(ctx, pool); err != nil {
		slog.Error("failed to apply migrations", "error", err)
		os.Exit(1)
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("db unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	slog.Info("forged metrics/admin listener starting", "addr", cfg.MetricsAddr)
	if err := http.ListenAndServe(cfg.MetricsAddr, nil); err != nil {
		slog.Error("metrics server failed", "error", err)
		os.Exit(1)
	}
}
