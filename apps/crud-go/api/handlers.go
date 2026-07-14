package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/troemmanuel/minipaas/apps/crud-go/internal/cache"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/db"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/metrics"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/models"
	"github.com/troemmanuel/minipaas/apps/crud-go/internal/queue"
)

type server struct {
	store *db.Store
	cache *cache.Cache
	queue *queue.Queue
}

type itemRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *server) publishEvent(eventType string, item models.Item) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.queue.PublishEvent(ctx, models.Event{Type: eventType, Item: item}); err != nil {
		log.Printf("warn: failed to publish %s event for item %d: %v", eventType, item.ID, err)
		return
	}
	metrics.EventsPublishedTotal.Inc()
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{}
	healthy := true

	if err := s.store.Ping(ctx); err != nil {
		checks["postgres"] = "down: " + err.Error()
		healthy = false
	} else {
		checks["postgres"] = "up"
	}

	if err := s.cache.Ping(ctx); err != nil {
		checks["redis"] = "down: " + err.Error()
		healthy = false
	} else {
		checks["redis"] = "up"
	}

	if s.queue.Healthy() {
		checks["rabbitmq"] = "up"
	} else {
		checks["rabbitmq"] = "down"
		healthy = false
	}

	status := http.StatusOK
	overall := "ok"
	if !healthy {
		status = http.StatusServiceUnavailable
		overall = "degraded"
	}
	writeJSON(w, status, map[string]any{"status": overall, "checks": checks})
}

func (s *server) handleListItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.List(r.Context())
	if err != nil {
		log.Printf("error: list items: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list items")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *server) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	item, err := s.store.Create(r.Context(), req.Name, req.Description)
	if err != nil {
		log.Printf("error: create item: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create item")
		return
	}

	s.publishEvent("item.created", item)
	writeJSON(w, http.StatusCreated, item)
}

func (s *server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if item, ok := s.cache.GetItem(r.Context(), id); ok {
		w.Header().Set("X-Cache", "HIT")
		writeJSON(w, http.StatusOK, item)
		return
	}

	item, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		log.Printf("error: get item: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get item")
		return
	}

	s.cache.SetItem(r.Context(), item)
	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, item)
}

func (s *server) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	item, err := s.store.Update(r.Context(), id, req.Name, req.Description)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		log.Printf("error: update item: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to update item")
		return
	}

	s.cache.InvalidateItem(r.Context(), id)
	s.publishEvent("item.updated", item)
	writeJSON(w, http.StatusOK, item)
}

func (s *server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := s.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		log.Printf("error: delete item: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to delete item")
		return
	}

	s.cache.InvalidateItem(r.Context(), id)
	s.publishEvent("item.deleted", models.Item{ID: id})
	w.WriteHeader(http.StatusNoContent)
}

func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
