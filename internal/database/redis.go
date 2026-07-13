package database

import (
	"context"
	"log"
	"time"

	"github.com/go-redis/redis/v8"

	"go_boilerplate/internal/config"
)

var redisClient *redis.Client

// InitRedis connects to Redis. In development, failure is non-fatal and returns nil
// so `go run .` still works without Redis (login/CRUD work; OTP/rate-limit limited).
// In production, missing Redis is fatal.
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
		log.Printf("WARNING: Redis not available at %s (%v)", redisAddr, err)
		log.Printf("WARNING: Continuing in development without Redis — OTP email verify and rate limits are limited.")
		log.Printf("WARNING: Install Memurai or start Redis, then restart. Login as admin still works.")
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
