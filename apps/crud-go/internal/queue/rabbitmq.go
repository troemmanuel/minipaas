package queue

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/troemmanuel/minipaas/apps/crud-go/internal/models"
)

type Queue struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	queueName string
}

func Connect(url, queueName string) (*Queue, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("declare queue: %w", err)
	}
	return &Queue{conn: conn, channel: ch, queueName: queueName}, nil
}

func (q *Queue) Close() error {
	if err := q.channel.Close(); err != nil {
		return err
	}
	return q.conn.Close()
}

// Healthy reports whether the underlying connection and channel are usable.
func (q *Queue) Healthy() bool {
	return q.conn != nil && !q.conn.IsClosed()
}

func (q *Queue) PublishEvent(ctx context.Context, event models.Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return q.channel.PublishWithContext(ctx, "", q.queueName, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
	})
}

// Consume returns a channel of deliveries for the configured queue.
func (q *Queue) Consume(consumerTag string) (<-chan amqp.Delivery, error) {
	if err := q.channel.Qos(10, 0, false); err != nil {
		return nil, fmt.Errorf("set qos: %w", err)
	}
	return q.channel.Consume(q.queueName, consumerTag, false, false, false, false, nil)
}
