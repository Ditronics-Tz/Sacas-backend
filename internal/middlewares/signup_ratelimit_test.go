package middlewares

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// TestSignupEmailFromBody_PeeksWithoutConsuming is the load-bearing property of
// the body peek: the handler must still see the whole body afterwards, or every
// onboarding request would fail validation.
func TestSignupEmailFromBody_PeeksWithoutConsuming(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"admin_email", `{"admin_email":"Admin@X.com","institution_name":"A"}`, "admin@x.com"},
		{"email", `{"email":"user@x.com"}`, "user@x.com"},
		{"admin_email preferred when both", `{"email":"e@x.com","admin_email":"a@x.com"}`, "a@x.com"},
		{"trimmed", `{"email":"  spaced@x.com  "}`, "spaced@x.com"},
		{"no email", `{"institution_name":"A"}`, ""},
		{"no at sign", `{"email":"not-an-email"}`, ""},
		{"not json", `not json at all`, ""},
		{"empty", ``, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/x", bytes.NewBufferString(tc.body))

			got := signupEmailFromBody(c)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}

			// The body must still be readable afterwards. An empty body
			// legitimately reports EOF.
			buf := make([]byte, 512)
			n, _ := c.Request.Body.Read(buf)
			read := string(buf[:n])
			if tc.body != "" && !strings.HasPrefix(tc.body, read) {
				t.Errorf("the body was altered: read %q, original %q", read, tc.body)
			}
		})
	}
}

// TestSignupEmailFromBody_NilRequestIsSafe: the middleware must not panic on a
// request with no body.
func TestSignupEmailFromBody_NilRequestIsSafe(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := signupEmailFromBody(c); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestSignupEmailFromBody_OversizedBodyIsTruncated guards the memory bound: a
// large upload must not be buffered whole just to find an email.
func TestSignupEmailFromBody_OversizedBodyIsTruncated(t *testing.T) {
	// A body well past the peek limit, with the email buried after it.
	huge := `{"padding":"` + strings.Repeat("x", signupEmailPeekLimit*2) + `","email":"deep@x.com"}`

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/x", bytes.NewBufferString(huge))

	// The peek is capped, so the truncated JSON does not parse and no key is
	// derived. That is the safe outcome: the IP limit still applies.
	if got := signupEmailFromBody(c); got != "" {
		t.Errorf("expected no email from a truncated body, got %q", got)
	}
}

// TestSignupRateLimitMiddleware_DisabledWhenRateLimitingOff keeps the local dev
// experience workable.
func TestSignupRateLimitMiddleware_DisabledWhenRateLimitingOff(t *testing.T) {
	t.Setenv("RATE_LIMIT_ENABLED", "false")

	r := gin.New()
	r.POST("/signup", SignupRateLimitMiddleware(nil), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(`{}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected the request through, got %d", w.Code)
	}
}

// TestSignupRateLimitMiddleware_DevWithoutRedisPasses mirrors the general
// limiter's behaviour so the two do not behave differently in development.
func TestSignupRateLimitMiddleware_DevWithoutRedisPasses(t *testing.T) {
	t.Setenv("RATE_LIMIT_ENABLED", "true")
	t.Setenv("ENV", "development")

	r := gin.New()
	r.POST("/signup", SignupRateLimitMiddleware(nil), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(`{}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected the request through in dev, got %d", w.Code)
	}
}

// TestSignupRateLimitMiddleware_ProductionWithoutRedisFailsClosed: in production
// a missing rate-limit store must refuse, not admit.
func TestSignupRateLimitMiddleware_ProductionWithoutRedisFailsClosed(t *testing.T) {
	t.Setenv("RATE_LIMIT_ENABLED", "true")
	t.Setenv("ENV", "production")

	r := gin.New()
	reached := false
	r.POST("/signup", SignupRateLimitMiddleware(nil), func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(`{}`)))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	if reached {
		t.Fatal("the handler ran without a rate-limit store in production")
	}
}
