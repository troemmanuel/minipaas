package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/troemmanuel/minipaas/apps/crud-go/internal/config"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/metrics"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/models"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/queue"
)

func main() {
	cfg := config.Load()

	q, err := queue.Connect(cfg.RabbitURL, cfg.QueueName)
	if err != nil {
		log.Fatalf("fatal: connect rabbitmq: %v", err)
	}
	defer q.Close()

	deliveries, err := q.Consume("crud-go-worker")
	if err != nil {
		log.Fatalf("fatal: consume queue: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go serveObservability(cfg.Port, q)

	log.Printf("worker listening on queue %q", cfg.QueueName)
	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down worker...")
			return
		case d, ok := <-deliveries:
			if !ok {
				log.Println("delivery channel closed, exiting")
				return
			}
			handleDelivery(d)
		}
	}
}

func handleDelivery(d amqp.Delivery) {
	var event models.Event
	if err := json.Unmarshal(d.Body, &event); err != nil {
		log.Printf("error: invalid event payload: %v", err)
		metrics.EventsConsumedTotal.WithLabelValues("unknown", "invalid").Inc()
		_ = d.Nack(false, false)
		return
	}

	log.Printf("processed event=%s item_id=%d name=%q", event.Type, event.Item.ID, event.Item.Name)
	metrics.EventsConsumedTotal.WithLabelValues(event.Type, "ok").Inc()
	_ = d.Ack(false)
}

// serveObservability exposes /health and /metrics for the worker so it can be scraped
// and probed independently from the API process.
func serveObservability(port string, q *queue.Queue) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if q.Healthy() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("error: observability server: %v", err)
	}
}
