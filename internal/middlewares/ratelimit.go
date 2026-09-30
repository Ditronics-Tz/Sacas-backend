package middlewares

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

// SignupRateLimitMiddleware is a much tighter per-IP limit for the public
// onboarding endpoints.
//
// It is separate from the general rate limit because those endpoints are far
// more expensive than a login check: each one can create a tenant row and an
// administrator. The general limit of 60/minute would still allow an attacker
// with a proxy pool to mint a large number of pending institutions, which then
// has to be triaged by a human.
//
// The window is deliberately long (per hour) because legitimate signups are rare
// and bursty — a new academic year, a new intake — and a per-minute limit would
// punish an institution onboarding a cohort at once.
//
// Also keyed on the email address where one is available, so an attacker cannot
// spread one address across many requests: the email key is a second, cheaper
// signal that fires before the IP limit would matter.
func SignupRateLimitMiddleware(redisClient *redis.Client) gin.HandlerFunc {
	perHour, _ := strconv.Atoi(config.GetEnv("SIGNUP_RATE_LIMIT_PER_HOUR", "5"))
	if perHour <= 0 {
		perHour = 5
	}

	return func(c *gin.Context) {
		if config.GetEnv("RATE_LIMIT_ENABLED", "true") != "true" {
			c.Next()
			return
		}
		// Dev without Redis: skip, matching the general limiter.
		if redisClient == nil {
			if config.GetEnv("ENV", "development") != "production" {
				c.Next()
				return
			}
			logger.Error("Signup rate limit enabled but Redis client is nil — fail closed")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Rate limit store unavailable",
			})
			c.Abort()
			return
		}

		now := time.Now().UTC()
		window := now.Format("2006010215") // per-hour bucket
		limits := []struct {
			prefix string
			value  string
		}{
			{"signup_ip", c.ClientIP()},
		}
		// An email-scoped key catches one address being spammed even from many
		// IPs, which the IP key alone would not see.
		if email := signupEmailFromBody(c); email != "" {
			limits = append(limits, struct {
				prefix string
				value  string
			}{"signup_email", email})
		}

		for _, limit := range limits {
			key := fmt.Sprintf("%s:%s:%s", limit.prefix, limit.value, window)
			n, err := redisClient.Incr(c, key).Result()
			if err != nil {
				if config.GetEnv("ENV", "development") != "production" {
					logger.Warn("Signup rate limit redis error in dev — allowing: %v", err)
					continue
				}
				logger.Error("Signup rate limit redis error: %v — fail closed", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": "Rate limit store unavailable",
				})
				c.Abort()
				return
			}
			if n == 1 {
				if expErr := redisClient.Expire(c, key, 2*time.Hour).Err(); expErr != nil {
					logger.Warn("Signup rate limit expire failed: %v", expErr)
				}
			}
			if int(n) > perHour {
				c.Header("Retry-After", "3600")
				c.JSON(http.StatusTooManyRequests, gin.H{
					"error": "Too many signup attempts from this source",
					"limit": perHour,
					"scope": "hour",
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// signupEmailFromBody peeks at the request body for an email to key the limit
// on, without consuming it.
//
// The body is restored immediately so the handler still sees it. A malformed
// body simply yields no email, and the IP limit still applies — this is a
// secondary signal, not the primary control.
func signupEmailFromBody(c *gin.Context) string {
	if c.Request == nil || c.Request.Body == nil {
		return ""
	}
	// A read error still leaves a usable body, so it is deliberately ignored:
	// the worst case is that no email key is derived and the IP limit still
	// applies.
	body, _ := io.ReadAll(io.LimitReader(c.Request.Body, signupEmailPeekLimit))
	// Restore so the handler still sees the full body.
	_ = c.Request.Body.Close()
	c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), c.Request.Body))

	var payload struct {
		Email      string `json:"email"`
		AdminEmail string `json:"admin_email"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	// admin_email is preferred over email: the institution-registration
	// endpoint is the more abuse-prone of the two, so when both are present the
	// key should track the address that endpoint actually uses.
	email := payload.AdminEmail
	if email == "" {
		email = payload.Email
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return ""
	}
	return email
}

// signupEmailPeekLimit caps how much of the body is buffered while peeking, so
// a large upload cannot be used to exhaust memory here.
const signupEmailPeekLimit = 8 << 10 // 8 KiB
