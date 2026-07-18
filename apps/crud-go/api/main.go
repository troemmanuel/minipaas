package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/troemmanuel/minipaas/apps/crud-go/internal/cache"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/config"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/db"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/metrics"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/queue"
)

func main() {
	cfg := config.Load()

	store, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("fatal: connect postgres: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(context.Background()); err != nil {
		log.Fatalf("fatal: migrate database: %v", err)
	}

	itemCache := cache.Connect(cfg.RedisAddr)
	defer itemCache.Close()

	q, err := queue.Connect(cfg.RabbitURL, cfg.QueueName)
	if err != nil {
		log.Fatalf("fatal: connect rabbitmq: %v", err)
	}
	defer q.Close()

	s := &server{store: store, cache: itemCache, queue: q}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", metrics.Instrument("/health", s.handleHealth))
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /items", metrics.Instrument("/items", s.handleListItems))
	mux.HandleFunc("POST /items", metrics.Instrument("/items", s.handleCreateItem))
	mux.HandleFunc("GET /items/{id}", metrics.Instrument("/items/{id}", s.handleGetItem))
	mux.HandleFunc("PUT /items/{id}", metrics.Instrument("/items/{id}", s.handleUpdateItem))
	mux.HandleFunc("DELETE /items/{id}", metrics.Instrument("/items/{id}", s.handleDeleteItem))

	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("api listening on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("fatal: http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down api...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("error: graceful shutdown: %v", err)
	}
}

// trigger CI pipeline test (etape 4) -- validation complete post-correctif kaniko
