package middlewares

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"go_boilerplate/internal/config"
	"go_boilerplate/pkg/logger"
)

// RateLimitMiddleware limits requests per IP using Redis fixed window.
// Applied to public auth/OTP routes. Fail-closed when Redis is unavailable
// so brute-force protection is not silently dropped.
func RateLimitMiddleware(redisClient *redis.Client) gin.HandlerFunc {
	rpm, _ := strconv.Atoi(config.GetEnv("RATE_LIMIT_REQUESTS_PER_MINUTE", "60"))
	if rpm <= 0 {
		rpm = 60
	}

	return func(c *gin.Context) {
		if config.GetEnv("RATE_LIMIT_ENABLED", "true") != "true" {
			c.Next()
			return
		}
		// Dev without Redis: skip limiting so local go run . still works.
		if redisClient == nil {
			if config.GetEnv("ENV", "development") != "production" {
				c.Next()
				return
			}
			logger.Error("Rate limit enabled but Redis client is nil — fail closed")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Rate limit store unavailable",
			})
			c.Abort()
			return
		}

		ip := c.ClientIP()
		window := time.Now().UTC().Format("200601021504") // per-minute bucket
		key := fmt.Sprintf("rl:%s:%s", ip, window)

		n, err := redisClient.Incr(c, key).Result()
		if err != nil {
			if config.GetEnv("ENV", "development") != "production" {
				logger.Warn("Rate limit redis error in dev — allowing request: %v", err)
				c.Next()
				return
			}
			logger.Error("Rate limit redis error: %v — fail closed", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Rate limit store unavailable",
			})
			c.Abort()
			return
		}
		if n == 1 {
			if expErr := redisClient.Expire(c, key, 2*time.Minute).Err(); expErr != nil {
				logger.Warn("Rate limit expire failed: %v", expErr)
			}
		}
		if int(n) > rpm {
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"limit": rpm,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
