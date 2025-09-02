package database

import (
	"context"
	"log"

	"github.com/go-redis/redis/v8"

	"go_boilerplate/internal/config"
)

var redisClient *redis.Client

func InitRedis() *redis.Client {
	redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := config.GetEnv("REDIS_PASSWORD", "")
	redisDB := 0 // Default DB

	redisClient = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})

	_, err := redisClient.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	return redisClient
}

func CloseRedis(client *redis.Client) {
	err := client.Close()
	if err != nil {
		log.Fatalf("Failed to close Redis connection: %v", err)
	}
}