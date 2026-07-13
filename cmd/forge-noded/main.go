package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"forge/internal/config"
	"forge/internal/logger"
	"forge/internal/nodemgr"
	"forge/internal/sandbox"
	"github.com/nats-io/nats.go"
	"log/slog"
)

func main() {
	logger.Init()
	cfg := config.LoadConfig()

	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		slog.Error("failed to connect to nats", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := nodemgr.NewWorker(nc, cfg)
	worker.Start(ctx)

	netMgr := sandbox.NewNetworkManager(cfg)
	sbxMgr := sandbox.NewManager(cfg, nc, netMgr)
	sbxMgr.HandleRPC(ctx)

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	go func() {
		slog.Info("forge-noded metrics/admin listener starting", "addr", cfg.MetricsAddr)
		if err := http.ListenAndServe(cfg.MetricsAddr, nil); err != nil {
			slog.Error("metrics server failed", "error", err)
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down forge-noded")
	cancel()
}
