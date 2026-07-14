package config

import "os"

// Config holds the runtime configuration read from the environment.
type Config struct {
	Port        string
	DatabaseURL string
	RedisAddr   string
	RabbitURL   string
	QueueName   string
}

func Load() Config {
	return Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://minipaas:minipaas@localhost:5432/minipaas?sslmode=disable"),
		RedisAddr:   getEnv("REDIS_ADDR", "localhost:6379"),
		RabbitURL:   getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		QueueName:   getEnv("RABBITMQ_QUEUE", "items_events"),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
