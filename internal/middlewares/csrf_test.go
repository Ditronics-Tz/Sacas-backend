package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// miniRedis is an in-memory stand-in for CSRF tests without a real Redis.
// We use miniredis if available; otherwise skip Redis-dependent cases.
// Here we test header-required logic with a nil Redis for fail-closed paths,
// and with a real redis client when REDIS_ADDR is set.

func TestCSRF_RejectsCookieOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Use redis if available; otherwise use a stub via miniredis-like map
	// We'll implement a lightweight fake using redis.NewClient against closed port
	// and instead unit-test the header-empty path with a working redis mock via
	// redis.NewClient + go-redis memory isn't available — use real protocol mock.

	// Simpler: test that missing header returns 403 even if cookie set, when Redis is nil
	// Redis nil → 503 before cookie check for mutate... actually header empty returns 403 first.
	r := gin.New()
	r.Use(CSRFMiddleware(CSRFConfig{RedisClient: nil, SkipPaths: nil}))
	r.POST("/api/test", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "abc123"})
	// no X-CSRF-Token header
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when header missing (cookie alone), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCSRF_RequiresHeaderEvenWithFormEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CSRFMiddleware(CSRFConfig{RedisClient: nil}))
	r.POST("/api/test", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without token, got %d", w.Code)
	}
}

func TestCSRF_HeaderCookieMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Need Redis for past header check — use a local redis or skip
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15})
	ctx := rdb.Context()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available for mismatch test")
	}
	defer rdb.FlushDB(ctx)

	token := "validtoken1234567890123456789012"
	_ = rdb.Set(ctx, "csrf:"+token, "valid", time.Hour).Err()

	r := gin.New()
	r.Use(CSRFMiddleware(CSRFConfig{RedisClient: rdb}))
	r.POST("/api/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set(CSRFHeaderName, token)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "different-cookie-value"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on header/cookie mismatch, got %d body=%s", w.Code, w.Body.String())
	}
}
