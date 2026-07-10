package main

import (
	"net/http"
	"os"

	"forge/internal/config"
	"forge/internal/logger"
	"log/slog"
)

func main() {
	logger.Init()
	cfg := config.LoadConfig()

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	slog.Info("forge-noded metrics/admin listener starting", "addr", cfg.MetricsAddr)
	if err := http.ListenAndServe(cfg.MetricsAddr, nil); err != nil {
		slog.Error("metrics server failed", "error", err)
		os.Exit(1)
	}
}
