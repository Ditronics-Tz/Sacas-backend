package middlewares

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"go_boilerplate/pkg/logger"
)

const (
	CSRFTokenLength = 32
	CSRFTokenTTL    = 24 * time.Hour
	CSRFHeaderName  = "X-CSRF-Token"
	CSRFCookieName  = "csrf_token"
)

type CSRFConfig struct {
	RedisClient *redis.Client
	SkipPaths   []string
}

// generateCSRFToken generates a cryptographically secure random token
func generateCSRFToken() (string, error) {
	bytes := make([]byte, CSRFTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CSRFMiddleware provides CSRF protection
func CSRFMiddleware(config CSRFConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip CSRF protection for specified paths
		for _, path := range config.SkipPaths {
			if c.Request.URL.Path == path {
				c.Next()
				return
			}
		}

		// Skip for GET, HEAD, OPTIONS methods
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			// Generate and set CSRF token for GET requests
			token, err := generateCSRFToken()
			if err != nil {
				logger.Error("Failed to generate CSRF token: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
				c.Abort()
				return
			}

			// Store token in Redis
			if config.RedisClient != nil {
				err = config.RedisClient.Set(c, "csrf:"+token, "valid", CSRFTokenTTL).Err()
				if err != nil {
					logger.Error("Failed to store CSRF token in Redis: %v", err)
				}
			}

			// Set CSRF token in cookie
			c.SetCookie(CSRFCookieName, token, int(CSRFTokenTTL.Seconds()), "/", "", false, true)
			c.Header(CSRFHeaderName, token)
			c.Next()
			return
		}

		// For POST, PUT, DELETE, PATCH methods, validate CSRF token
		token := c.GetHeader(CSRFHeaderName)
		if token == "" {
			// Try to get token from form data
			token = c.PostForm("csrf_token")
		}

		if token == "" {
			logger.Warn("CSRF token missing for %s %s", c.Request.Method, c.Request.URL.Path)
			c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token required"})
			c.Abort()
			return
		}

		// Validate token against Redis
		if config.RedisClient != nil {
			val, err := config.RedisClient.Get(c, "csrf:"+token).Result()
			if err == redis.Nil {
				logger.Warn("Invalid or expired CSRF token for %s %s", c.Request.Method, c.Request.URL.Path)
				c.JSON(http.StatusForbidden, gin.H{"error": "Invalid or expired CSRF token"})
				c.Abort()
				return
			} else if err != nil {
				logger.Error("Failed to validate CSRF token: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
				c.Abort()
				return
			}

			if val != "valid" {
				logger.Warn("Invalid CSRF token value for %s %s", c.Request.Method, c.Request.URL.Path)
				c.JSON(http.StatusForbidden, gin.H{"error": "Invalid CSRF token"})
				c.Abort()
				return
			}

			// Token is valid, delete it to prevent reuse
			config.RedisClient.Del(c, "csrf:"+token)
		}

		c.Next()
	}
}