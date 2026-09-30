package middlewares

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// userLookup builds an ActiveUserLookup over a fixed map.
func userLookup(users map[uint]*models.User) ActiveUserLookup {
	return func(id uint) (*models.User, error) {
		u, ok := users[id]
		if !ok {
			return nil, errors.New("not found")
		}
		cp := *u
		return &cp, nil
	}
}

// instLookup builds a TenantLookup over a fixed map.
func instLookup(insts map[uint]*models.Institution) TenantLookup {
	return func(id uint) (*models.Institution, error) {
		i, ok := insts[id]
		if !ok {
			return nil, errors.New("not found")
		}
		cp := *i
		return &cp, nil
	}
}

func activeInst(id uint) *models.Institution {
	return &models.Institution{
		ID: id, Name: "Inst", Slug: "inst",
		Status: models.InstitutionStatusActive,
		Plan:   models.InstitutionPlanFree,
		Type:   models.InstitutionTypeCollege,
	}
}

func ptr[T any](v T) *T { return &v }

// TestTenantMiddleware_ResolvesInstitutionFromDB covers the central contract:
// the institution and the role come from the user's database row, overriding
// whatever the token claimed.
func TestTenantMiddleware_ResolvesInstitutionFromDB(t *testing.T) {
	users := map[uint]*models.User{
		1: {
			ID: 1, Email: "a@x.com", Role: models.RoleAdmin, IsActive: true,
			InstitutionID: ptr(uint(7)),
		},
	}
	insts := map[uint]*models.Institution{7: activeInst(7)}

	var got uint
	var gotRole string
	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) {
			// Simulate a stale/hostile token claim. The DB row must win.
			c.Set(ContextKeyUserID, float64(1))
			c.Set(ContextKeyRole, "user")
			c.Set(ContextKeyEmail, "attacker@evil.com")
		},
		TenantMiddleware(userLookup(users), instLookup(insts)),
		func(c *gin.Context) {
			got = InstitutionIDFromContext(c)
			gotRole = RoleFromContext(c)
			c.Status(http.StatusOK)
		},
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got != 7 {
		t.Fatalf("expected institution 7, got %d", got)
	}
	// The role must come from the database, not the token.
	if gotRole != string(models.RoleAdmin) {
		t.Fatalf("expected role %q from DB, got %q", models.RoleAdmin, gotRole)
	}
}

func TestTenantMiddleware_PlatformSuperAdminGetsPlatformScope(t *testing.T) {
	users := map[uint]*models.User{
		1: {ID: 1, Email: "root@x.com", Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil},
	}

	var got uint
	var isPlatform bool
	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) { c.Set(ContextKeyUserID, float64(1)) },
		TenantMiddleware(userLookup(users), instLookup(nil)),
		func(c *gin.Context) {
			got = InstitutionIDFromContext(c)
			isPlatform = IsPlatformUser(c)
			c.Status(http.StatusOK)
		},
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got != PlatformScope {
		t.Fatalf("expected platform scope, got %d", got)
	}
	if !isPlatform {
		t.Fatal("expected IsPlatformUser to be true")
	}
}

func TestTenantMiddleware_RejectsSuspendedInstitution(t *testing.T) {
	users := map[uint]*models.User{
		1: {ID: 1, Role: models.RoleAdmin, IsActive: true, InstitutionID: ptr(uint(7))},
	}
	suspended := activeInst(7)
	suspended.Status = models.InstitutionStatusSuspended
	insts := map[uint]*models.Institution{7: suspended}

	r := gin.New()
	reached := false
	r.GET("/x",
		func(c *gin.Context) { c.Set(ContextKeyUserID, float64(1)) },
		TenantMiddleware(userLookup(users), instLookup(insts)),
		func(c *gin.Context) { reached = true; c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a suspended institution, got %d", w.Code)
	}
	if reached {
		t.Fatal("handler ran for a suspended institution")
	}
}

func TestTenantMiddleware_RejectsPendingInstitution(t *testing.T) {
	users := map[uint]*models.User{
		1: {ID: 1, Role: models.RoleAdmin, IsActive: true, InstitutionID: ptr(uint(7))},
	}
	pending := activeInst(7)
	pending.Status = models.InstitutionStatusPending
	insts := map[uint]*models.Institution{7: pending}

	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) { c.Set(ContextKeyUserID, float64(1)) },
		TenantMiddleware(userLookup(users), instLookup(insts)),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a pending institution, got %d", w.Code)
	}
}

func TestTenantMiddleware_RejectsExpiredTrial(t *testing.T) {
	users := map[uint]*models.User{
		1: {ID: 1, Role: models.RoleAdmin, IsActive: true, InstitutionID: ptr(uint(7))},
	}
	expired := activeInst(7)
	expired.TrialEndsAt = ptr(time.Now().Add(-24 * time.Hour))
	insts := map[uint]*models.Institution{7: expired}

	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) { c.Set(ContextKeyUserID, float64(1)) },
		TenantMiddleware(userLookup(users), instLookup(insts)),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an expired trial, got %d", w.Code)
	}
}

func TestTenantMiddleware_RejectsInactiveUser(t *testing.T) {
	users := map[uint]*models.User{
		1: {ID: 1, Role: models.RoleAdmin, IsActive: false, InstitutionID: ptr(uint(7))},
	}
	insts := map[uint]*models.Institution{7: activeInst(7)}

	r := gin.New()
	reached := false
	r.GET("/x",
		func(c *gin.Context) { c.Set(ContextKeyUserID, float64(1)) },
		TenantMiddleware(userLookup(users), instLookup(insts)),
		func(c *gin.Context) { reached = true; c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an inactive user, got %d", w.Code)
	}
	if reached {
		t.Fatal("handler ran for an inactive user")
	}
}

// TestTenantMiddleware_RejectsNonSuperAdminWithoutInstitution covers the
// misconfiguration guard: an ordinary user with no institution is refused
// rather than silently treated as a platform operator.
func TestTenantMiddleware_RejectsNonSuperAdminWithoutInstitution(t *testing.T) {
	users := map[uint]*models.User{
		1: {ID: 1, Role: models.RoleUser, IsActive: true, InstitutionID: nil},
	}

	r := gin.New()
	reached := false
	r.GET("/x",
		func(c *gin.Context) { c.Set(ContextKeyUserID, float64(1)) },
		TenantMiddleware(userLookup(users), instLookup(nil)),
		func(c *gin.Context) { reached = true; c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a user with no institution, got %d", w.Code)
	}
	if reached {
		t.Fatal("handler ran for a user with no institution")
	}
}

func TestTenantMiddleware_RejectsUnknownUser(t *testing.T) {
	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) { c.Set(ContextKeyUserID, float64(99)) },
		TenantMiddleware(userLookup(map[uint]*models.User{}), instLookup(nil)),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unknown user, got %d", w.Code)
	}
}

func TestRequirePlatformOnly(t *testing.T) {
	build := func(institutionID uint, role string) *gin.Engine {
		r := gin.New()
		r.GET("/x",
			func(c *gin.Context) {
				SetTenant(c, institutionID, role)
			},
			RequirePlatformOnly(),
			func(c *gin.Context) { c.Status(http.StatusOK) },
		)
		return r
	}

	t.Run("platform super_admin allowed", func(t *testing.T) {
		w := httptest.NewRecorder()
		build(PlatformScope, string(models.RoleSuperAdmin)).
			ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("institution-bound super_admin rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		build(7, string(models.RoleSuperAdmin)).
			ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})

	t.Run("tenant admin rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		build(7, string(models.RoleAdmin)).
			ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Dar es Salaam University": "dar-es-salaam-university",
		"  College  ":              "college",
		"Nursing & Midwifery":      "nursing-midwifery",
		"St. Augustine's":          "st-augustine-s",
		"":                         "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInstitutionIDFromContext_Coercions(t *testing.T) {
	// The value may arrive as any numeric type depending on the caller, so the
	// accessor must coerce rather than fail.
	for _, v := range []any{uint(5), uint64(5), int(5), int64(5), float64(5), "5"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(ContextKeyInstitutionID, v)
		if got := InstitutionIDFromContext(c); got != 5 {
			t.Errorf("for %T(%v): got %d, want 5", v, v, got)
		}
	}

	// An unset key is the platform scope, not a random institution.
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := InstitutionIDFromContext(c); got != PlatformScope {
		t.Errorf("unset institution: got %d, want platform scope", got)
	}
}
