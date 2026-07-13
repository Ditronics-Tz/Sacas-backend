package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"go_boilerplate/internal/models"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func signTestToken(t *testing.T, role string, secret string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(1),
		"email":   "test@example.com",
		"role":    role,
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	})
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func testRouterWithSecret(secret string, register func(r *gin.Engine)) *gin.Engine {
	// Point JWT middleware at test secret via env is heavy; instead inject claims
	// through a test double that mimics JWTAuthMiddleware output.
	r := gin.New()
	register(r)
	return r
}

// attachRole pretends JWT middleware already verified the token.
func attachRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", float64(1))
		c.Set("email", "test@example.com")
		c.Set("role", role)
		c.Next()
	}
}

func TestAdminMiddleware_roleMatrix(t *testing.T) {
	cases := []struct {
		name       string
		role       string
		wantStatus int
	}{
		{"user forbidden", string(models.RoleUser), http.StatusForbidden},
		{"admin allowed", string(models.RoleAdmin), http.StatusOK},
		{"super_admin allowed", string(models.RoleSuperAdmin), http.StatusOK},
		{"empty role forbidden", "", http.StatusForbidden},
		{"unknown role forbidden", "hacker", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/timetable/faculties", attachRole(tc.role), AdminMiddleware(), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})
			req := httptest.NewRequest(http.MethodGet, "/timetable/faculties", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("role=%q status=%d want=%d body=%s", tc.role, w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestSuperAdminMiddleware_roleMatrix(t *testing.T) {
	cases := []struct {
		role       string
		wantStatus int
	}{
		{string(models.RoleUser), http.StatusForbidden},
		{string(models.RoleAdmin), http.StatusForbidden},
		{string(models.RoleSuperAdmin), http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			r := gin.New()
			r.GET("/superadmin/system/info", attachRole(tc.role), SuperAdminMiddleware(), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})
			req := httptest.NewRequest(http.MethodGet, "/superadmin/system/info", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("role=%q status=%d want=%d", tc.role, w.Code, tc.wantStatus)
			}
		})
	}
}

func TestRequireRole_readsOnlyContextNotHeaders(t *testing.T) {
	r := gin.New()
	// Client tries to spoof role via header — middleware must ignore it
	r.GET("/secure", attachRole(string(models.RoleUser)), RequireRole(string(models.RoleAdmin)), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("X-Role", "super_admin")
	req.Header.Set("Role", "administrator")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("spoof header must not grant access, got %d", w.Code)
	}
}

func TestJWTAuthMiddleware_rejectsMissingToken(t *testing.T) {
	r := gin.New()
	r.GET("/p", JWTAuthMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestJWTAuthMiddleware_acceptsBearerAndSetsRole(t *testing.T) {
	secret := "test-jwt-secret-for-rbac-unit-tests-32"
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("ENV", "development")

	token := signTestToken(t, string(models.RoleAdmin), secret)
	r := gin.New()
	r.GET("/p", JWTAuthMiddleware(), AdminMiddleware(), func(c *gin.Context) {
		role, _ := c.Get("role")
		c.JSON(http.StatusOK, gin.H{"role": role})
	})
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestJWTAuthMiddleware_userCannotPassAdmin(t *testing.T) {
	secret := "test-jwt-secret-for-rbac-unit-tests-32"
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("ENV", "development")

	token := signTestToken(t, string(models.RoleUser), secret)
	r := gin.New()
	r.GET("/timetable/rooms", JWTAuthMiddleware(), AdminMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	req := httptest.NewRequest(http.MethodGet, "/timetable/rooms", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user token must get 403 on admin route, got %d", w.Code)
	}
}

// silence unused helper in case build tags change
var _ = testRouterWithSecret
