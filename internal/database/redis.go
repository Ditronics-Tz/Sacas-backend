package database

import (
	"context"
	"log"
	"time"

	"github.com/go-redis/redis/v8"

	"go_boilerplate/internal/config"
)

var redisClient *redis.Client

// InitRedis connects to Redis. In development, failure is non-fatal and returns nil.
func InitRedis() *redis.Client {
	redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := config.GetEnv("REDIS_PASSWORD", "")

	client := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     redisPassword,
		DB:           0,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := client.Ping(ctx).Result(); err != nil {
		_ = client.Close()
		env := config.GetEnv("ENV", "development")
		if env == "production" {
			log.Fatalf("Failed to connect to Redis at %s: %v", redisAddr, err)
		}
		// One quiet line only
		log.Printf("redis: offline (%s) — continuing without OTP/rate-limit store", redisAddr)
		redisClient = nil
		return nil
	}

	redisClient = client
	return redisClient
}

func CloseRedis(client *redis.Client) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		log.Printf("Failed to close Redis connection: %v", err)
	}
}
