package middlewares

import (
	"crypto/rand"
	"crypto/subtle"
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

func generateCSRFToken() (string, error) {
	bytes := make([]byte, CSRFTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// IssueCSRFToken stores a new token in Redis and sets cookie + response header.
// Used by middleware on safe methods and by GET /api/csrf.
func IssueCSRFToken(c *gin.Context, redisClient *redis.Client) (string, error) {
	if redisClient == nil {
		return "", errCSRFStoreUnavailable
	}
	token, err := generateCSRFToken()
	if err != nil {
		return "", err
	}
	if err := redisClient.Set(c, "csrf:"+token, "valid", CSRFTokenTTL).Err(); err != nil {
		return "", err
	}
	// non-HttpOnly so SPA can read cookie for double-submit mirror if needed;
	// primary path is response header X-CSRF-Token.
	c.SetCookie(CSRFCookieName, token, int(CSRFTokenTTL.Seconds()), "/", "", false, false)
	c.Header(CSRFHeaderName, token)
	return token, nil
}

var errCSRFStoreUnavailable = &csrfStoreError{}

type csrfStoreError struct{}

func (e *csrfStoreError) Error() string { return "CSRF store unavailable" }

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// CSRFMiddleware provides CSRF protection for SPAs:
// - Issues token on safe methods via header X-CSRF-Token and non-HttpOnly cookie
// - Requires Redis for issue/validate (fail closed if Redis missing/down)
// - Mutating requests MUST send X-CSRF-Token (or form csrf_token); cookie alone is NOT enough
// - When csrf_token cookie is present, header must match it (double-submit)
// - Token remains valid for TTL (not burned per request)
func CSRFMiddleware(config CSRFConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, path := range config.SkipPaths {
			if c.Request.URL.Path == path {
				c.Next()
				return
			}
		}

		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			if _, err := IssueCSRFToken(c, config.RedisClient); err != nil {
				logger.Error("Failed to issue CSRF token: %v", err)
				status := http.StatusInternalServerError
				if err == errCSRFStoreUnavailable || config.RedisClient == nil {
					status = http.StatusServiceUnavailable
				}
				// OPTIONS preflight should still complete CORS; only fail body GETs hard
				if c.Request.Method == "OPTIONS" {
					c.Next()
					return
				}
				c.JSON(status, gin.H{"error": "CSRF store unavailable"})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// --- mutating methods: require explicit header/form token (not cookie alone) ---
		headerToken := c.GetHeader(CSRFHeaderName)
		if headerToken == "" {
			headerToken = c.PostForm("csrf_token")
		}
		if headerToken == "" {
			logger.Warn("CSRF header missing for %s %s", c.Request.Method, c.Request.URL.Path)
			c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token required (send X-CSRF-Token header)"})
			c.Abort()
			return
		}

		// Double-submit: if browser sent csrf cookie, header must match it
		if cookieToken, err := c.Cookie(CSRFCookieName); err == nil && cookieToken != "" {
			if !secureEqual(headerToken, cookieToken) {
				logger.Warn("CSRF header/cookie mismatch for %s %s", c.Request.Method, c.Request.URL.Path)
				c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token mismatch"})
				c.Abort()
				return
			}
		}

		if config.RedisClient == nil {
			logger.Error("CSRF validation requested without Redis — fail closed")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "CSRF store unavailable"})
			c.Abort()
			return
		}

		val, err := config.RedisClient.Get(c, "csrf:"+headerToken).Result()
		if err == redis.Nil || val != "valid" {
			logger.Warn("Invalid or expired CSRF token for %s %s", c.Request.Method, c.Request.URL.Path)
			c.JSON(http.StatusForbidden, gin.H{"error": "Invalid or expired CSRF token"})
			c.Abort()
			return
		} else if err != nil {
			logger.Error("Failed to validate CSRF token: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "CSRF store unavailable"})
			c.Abort()
			return
		}

		c.Next()
	}
}
