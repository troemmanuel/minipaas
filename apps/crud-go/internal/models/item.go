package models

import "time"

// Item is the CRUD resource exposed by the API.
type Item struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Event is published to RabbitMQ whenever an item is created, updated or deleted.
type Event struct {
	Type string `json:"type"`
	Item Item   `json:"item"`
}
