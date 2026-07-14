package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/troemmanuel/minipaas/apps/crud-go/internal/models"
)

const itemTTL = 60 * time.Second

type Cache struct {
	client *redis.Client
}

func Connect(addr string) *Cache {
	return &Cache{client: redis.NewClient(&redis.Options{Addr: addr})}
}

func (c *Cache) Close() error {
	return c.client.Close()
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func itemKey(id int64) string {
	return fmt.Sprintf("item:%d", id)
}

func (c *Cache) GetItem(ctx context.Context, id int64) (models.Item, bool) {
	raw, err := c.client.Get(ctx, itemKey(id)).Bytes()
	if err != nil {
		return models.Item{}, false
	}
	var item models.Item
	if err := json.Unmarshal(raw, &item); err != nil {
		return models.Item{}, false
	}
	return item, true
}

func (c *Cache) SetItem(ctx context.Context, item models.Item) {
	raw, err := json.Marshal(item)
	if err != nil {
		return
	}
	c.client.Set(ctx, itemKey(item.ID), raw, itemTTL)
}

func (c *Cache) InvalidateItem(ctx context.Context, id int64) {
	c.client.Del(ctx, itemKey(id))
}
