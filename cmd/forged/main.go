package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"forge/internal/api"
	"forge/internal/auth"
	"forge/internal/config"
	"forge/internal/credits"
	"forge/internal/db"
	"forge/internal/events"
	"forge/internal/logger"
	"forge/internal/scheduler"
	"forge/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
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

	// Initialize stores & managers
	userStore := store.NewUserStore(pool)
	sessionStore := store.NewSessionStore(pool)
	projectStore := store.NewProjectStore(pool)
	ledgerStore := store.NewLedgerStore(pool)
	credMgr := credits.NewManager(pool)

	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		slog.Error("failed to connect to nats", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	// Scheduler
	schedulerMgr := scheduler.NewScheduler(pool, nc)
	schedulerMgr.Start(ctx)

	// EventBus
	eventBus := events.NewEventBus(pool, nc)
	eventBus.Start(ctx)

	// Rate limiters
	rateLimiters := map[string]func(http.Handler) http.Handler{
		"signup": api.NewRateLimiter(5, time.Hour).Middleware(api.ExtractIP),
		"login":  api.NewRateLimiter(10, 15*time.Minute).Middleware(api.ExtractIP),
		// ... more as needed
	}

	// Handlers
	authHandler := api.NewAuthHandler(userStore, sessionStore, ledgerStore, credMgr, cfg)
	projectsHandler := api.NewProjectsHandler(projectStore, userStore, cfg, eventBus)
	adminHandler := api.NewAdminHandler(userStore, credMgr)

	// Main Router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/v1", func(r chi.Router) {
		authHandler.Register(r, rateLimiters)

		r.Group(func(r chi.Router) {
			r.Use(auth.SessionMiddleware(sessionStore, userStore))
			r.Use(auth.CSRFMiddleware) // all mutations need CSRF

			r.Route("/projects", func(r chi.Router) {
				projectsHandler.MountRoutes(r)
			})

			r.Route("/admin", func(r chi.Router) {
				r.Use(auth.AdminMiddleware(userStore))
				adminHandler.MountRoutes(r)
			})
		})
	})

	// Add missing routes that were originally in main.go
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

	// Internal/Metrics listener
	go func() {
		slog.Info("forged metrics/admin listener starting", "addr", cfg.MetricsAddr)
		if err := http.ListenAndServe(cfg.MetricsAddr, nil); err != nil {
			slog.Error("metrics server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Public listener
	slog.Info("forged public api listener starting", "addr", ":8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		slog.Error("public server failed", "error", err)
		os.Exit(1)
	}
}
