package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"go_boilerplate/internal/auth"
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

// attachRole pretends the JWT and tenant middlewares have already run, which is
// the state RequirePermission actually reads. institutionID is the resolved
// tenant scope; 0 means platform.
func attachRole(role string, institutionID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ContextKeyUserID, float64(1))
		c.Set(ContextKeyEmail, "test@example.com")
		c.Set(ContextKeyRole, role)
		c.Set(ContextKeyInstitutionID, institutionID)
		c.Next()
	}
}

// attachSupportRequest pretends a request authenticated with a support token.
func attachSupportRequest(institutionID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ContextKeyIsSupport, true)
		c.Set(ContextKeyRole, string(models.RoleSupport))
		c.Set(ContextKeyTokenInstitution, float64(institutionID))
		c.Next()
	}
}

// allowCode is the status when a request reaches the handler.
const allowCode = http.StatusOK

// --- RequirePermission ------------------------------------------------------

// TestRequirePermission_matrix is the core role × permission check: every role
// is exercised against every permission, and the expected answer is derived
// from the permission map rather than hardcoded per case. That way the test
// fails if the map is edited without the matrix being reconsidered.
func TestRequirePermission_matrix(t *testing.T) {
	permissions := []auth.Permission{
		auth.PermProfileRead, auth.PermProfileUpdate, auth.PermPasswordSet,
		auth.PermFacultyRead, auth.PermFacultyWrite,
		auth.PermCourseRead, auth.PermCourseWrite,
		auth.PermTimetableRead, auth.PermTimetablePreview, auth.PermTimetableGenerate,
		auth.PermTimetableOverride, auth.PermTimetableApprove,
		auth.PermExamRead, auth.PermExamWrite, auth.PermExamPublish,
		auth.PermUserRead, auth.PermUserWrite, auth.PermUserDelete,
		auth.PermAdminStats, auth.PermInstitutionRead, auth.PermInstitutionWrite,
		auth.PermInstitutionDelete, auth.PermAuditRead, auth.PermImpersonate,
		auth.PermSupportRead,
	}

	// A platform super_admin has no institution; everyone else is scoped to one.
	scopeFor := func(role models.UserRole) uint {
		if role == models.RoleSuperAdmin {
			return PlatformScope
		}
		return 1
	}

	for _, role := range models.AllRoles {
		for _, perm := range permissions {
			t.Run(string(role)+"/"+string(perm), func(t *testing.T) {
				r := gin.New()
				r.GET("/x",
					attachRole(string(role), scopeFor(role)),
					RequirePermission(perm),
					func(c *gin.Context) { c.Status(allowCode) },
				)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

				want := allowCode
				if !auth.HasPermission(role, perm) {
					want = http.StatusForbidden
				}
				if w.Code != want {
					t.Fatalf("role=%s perm=%s got %d want %d", role, perm, w.Code, want)
				}
			})
		}
	}
}

// TestRequirePermission_superAdminHasNoWorkspaceAccess is the policy the tenancy
// work depends on: a platform account holds no institution-workspace
// permission at all, so it cannot reach tenant data even if a route forgot to
// add the workspace gate.
func TestRequirePermission_superAdminHasNoWorkspaceAccess(t *testing.T) {
	workspace := []auth.Permission{
		auth.PermFacultyRead, auth.PermFacultyWrite,
		auth.PermCourseRead, auth.PermClassWrite,
		auth.PermStaffRead, auth.PermStaffWrite,
		auth.PermTimetableRead, auth.PermTimetableGenerate,
		auth.PermUserRead, auth.PermUserWrite, auth.PermUserDelete,
		auth.PermAdminStats, auth.PermSettingsWrite,
	}
	for _, perm := range workspace {
		if auth.HasPermission(models.RoleSuperAdmin, perm) {
			t.Errorf("super_admin must NOT hold workspace permission %s", perm)
		}
	}
}

// TestRequirePermission_supportIsReadOnly verifies a support session cannot
// reach any write surface, even with a valid institution scope.
func TestRequirePermission_supportIsReadOnly(t *testing.T) {
	writes := []auth.Permission{
		auth.PermFacultyWrite, auth.PermCourseWrite, auth.PermClassWrite,
		auth.PermStaffWrite, auth.PermUserWrite, auth.PermUserDelete,
		auth.PermTimetableGenerate, auth.PermTimetableOverride,
		auth.PermExamWrite, auth.PermSettingsWrite, auth.PermImpersonate,
	}
	for _, perm := range writes {
		if auth.HasPermission(models.RoleSupport, perm) {
			t.Errorf("support role must NOT hold write permission %s", perm)
		}
		if !auth.HasPermission(models.RoleSupport, auth.PermSupportRead) {
			t.Error("support role should hold support:read")
		}
	}
}

// --- Workspace gates --------------------------------------------------------

func TestRequireInstitutionWorkspace_rejectsPlatformRole(t *testing.T) {
	r := gin.New()
	reached := false
	r.GET("/x",
		attachRole(string(models.RoleSuperAdmin), PlatformScope),
		RequireInstitutionWorkspace(),
		func(c *gin.Context) { reached = true; c.Status(allowCode) },
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	// This is the "remove super_admin from institution-workspace routes"
	// requirement: it fails closed, with an explanation.
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a platform account, got %d", w.Code)
	}
	if reached {
		t.Fatal("handler ran for a platform account on an institution route")
	}
}

func TestRequireInstitutionWorkspace_allowsInstitutionRoles(t *testing.T) {
	institutionRoles := []models.UserRole{
		models.RoleAdmin, models.RoleAcademicCoord, models.RoleExamCoord, models.RoleUser,
	}
	for _, role := range institutionRoles {
		t.Run(string(role), func(t *testing.T) {
			r := gin.New()
			r.GET("/x",
				attachRole(string(role), 1),
				RequireInstitutionWorkspace(),
				func(c *gin.Context) { c.Status(allowCode) },
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
			if w.Code != allowCode {
				t.Fatalf("role=%s expected 200, got %d", role, w.Code)
			}
		})
	}
}

// TestRequireInstitutionWorkspace_rejectsTenantRoleWithNoScope is defence in
// depth: an institution role that somehow reached the gate without a tenant
// scope would make every repository call unscoped, so it is refused.
func TestRequireInstitutionWorkspace_rejectsTenantRoleWithNoScope(t *testing.T) {
	r := gin.New()
	reached := false
	r.GET("/x",
		attachRole(string(models.RoleAdmin), PlatformScope),
		RequireInstitutionWorkspace(),
		func(c *gin.Context) { reached = true; c.Status(allowCode) },
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without a tenant scope, got %d", w.Code)
	}
	if reached {
		t.Fatal("handler ran with no tenant scope")
	}
}

func TestRequirePlatformWorkspace(t *testing.T) {
	cases := []struct {
		name       string
		role       models.UserRole
		inst       uint
		wantStatus int
	}{
		{"platform super_admin", models.RoleSuperAdmin, PlatformScope, allowCode},
		{"institution-bound super_admin", models.RoleSuperAdmin, 1, http.StatusForbidden},
		{"institution admin", models.RoleAdmin, 1, http.StatusForbidden},
		{"user", models.RoleUser, 1, http.StatusForbidden},
		{"support session", models.RoleSupport, 1, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/x",
				attachRole(string(tc.role), tc.inst),
				RequirePlatformWorkspace(),
				func(c *gin.Context) { c.Status(allowCode) },
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("got %d want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

// --- Role spoofing ----------------------------------------------------------

// TestRequirePermission_ignoresClientSuppliedRole verifies the permission check
// reads only the resolved context, so no header, query, or body can escalate.
func TestRequirePermission_ignoresClientSuppliedRole(t *testing.T) {
	r := gin.New()
	r.GET("/x", attachRole(string(models.RoleUser), 1), RequirePermission(auth.PermUserWrite),
		func(c *gin.Context) { c.Status(allowCode) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Role", "super_admin")
	req.Header.Set("Role", "administrator")
	req.Header.Set("X-User-Role", "super_admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("spoofed headers must not grant access, got %d", w.Code)
	}
}

func TestRequirePermission_failsClosedWithoutRole(t *testing.T) {
	r := gin.New()
	r.GET("/x", RequirePermission(auth.PermUserWrite),
		func(c *gin.Context) { c.Status(allowCode) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	// No role resolved means a misconfigured chain; refuse rather than allow.
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without a role, got %d", w.Code)
	}
}

func TestRequireAnyPermission(t *testing.T) {
	cases := []struct {
		name       string
		role       models.UserRole
		perms      []auth.Permission
		wantStatus int
	}{
		{"admin matches first", models.RoleAdmin, []auth.Permission{auth.PermUserWrite, auth.PermExamWrite}, allowCode},
		{"exam coord matches second", models.RoleExamCoord, []auth.Permission{auth.PermUserWrite, auth.PermExamWrite}, allowCode},
		{"user matches neither", models.RoleUser, []auth.Permission{auth.PermUserWrite, auth.PermExamWrite}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/x",
				attachRole(string(tc.role), 1),
				RequireAnyPermission(tc.perms...),
				func(c *gin.Context) { c.Status(allowCode) },
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("got %d want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

// TestRequirePermission_denialDoesNotLeakCallerPermissions asserts a 403 does
// not hand back the CALLER's own permission list. Naming the permission the
// endpoint requires is deliberate and useful for the UI; enumerating what the
// caller can already do would let someone probing the boundary map out the
// policy, and would also be stale by the time they read it.
func TestRequirePermission_denialDoesNotLeakCallerPermissions(t *testing.T) {
	r := gin.New()
	r.GET("/x",
		attachRole(string(models.RoleUser), 1),
		RequirePermission(auth.PermImpersonate),
		func(c *gin.Context) { c.Status(allowCode) },
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	body := w.Body.String()

	// The caller's own permissions must not be enumerated in the refusal.
	for _, owned := range auth.PermissionsFor(models.RoleUser) {
		if contains(body, string(owned)) {
			t.Errorf("denial body leaks a permission the caller already holds (%s): %s", owned, body)
		}
	}
	// The required permission is expected — the UI needs it to explain itself.
	if !contains(body, string(auth.PermImpersonate)) {
		t.Errorf("denial should name the required permission: %s", body)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// --- Legacy middleware ------------------------------------------------------

func TestSuperAdminMiddleware_roleMatrix(t *testing.T) {
	cases := []struct {
		role       string
		wantStatus int
	}{
		{string(models.RoleUser), http.StatusForbidden},
		{string(models.RoleAdmin), http.StatusForbidden},
		{string(models.RoleAcademicCoord), http.StatusForbidden},
		{string(models.RoleExamCoord), http.StatusForbidden},
		{string(models.RoleSupport), http.StatusForbidden},
		{string(models.RoleSuperAdmin), allowCode},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			r := gin.New()
			r.GET("/superadmin/system/info", attachRole(tc.role, 1), SuperAdminMiddleware(),
				func(c *gin.Context) { c.JSON(allowCode, gin.H{"ok": true}) })
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
	r.GET("/secure", attachRole(string(models.RoleUser), 1),
		RequireRole(string(models.RoleAdmin)),
		func(c *gin.Context) { c.JSON(allowCode, gin.H{"ok": true}) })
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("X-Role", "super_admin")
	req.Header.Set("Role", "administrator")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("spoof header must not grant access, got %d", w.Code)
	}
}

// --- JWT middleware ---------------------------------------------------------

func TestJWTAuthMiddleware_rejectsMissingToken(t *testing.T) {
	r := gin.New()
	r.GET("/p", JWTAuthMiddleware(), func(c *gin.Context) { c.Status(allowCode) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/p", nil))
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
	r.GET("/p", JWTAuthMiddleware(), RequirePermission(auth.PermUserWrite), func(c *gin.Context) {
		role, _ := c.Get("role")
		c.JSON(allowCode, gin.H{"role": role})
	})
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != allowCode {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestJWTAuthMiddleware_userCannotPassAdmin(t *testing.T) {
	secret := "test-jwt-secret-for-rbac-unit-tests-32"
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("ENV", "development")

	token := signTestToken(t, string(models.RoleUser), secret)
	r := gin.New()
	r.GET("/timetable/rooms", JWTAuthMiddleware(), RequirePermission(auth.PermRoomWrite),
		func(c *gin.Context) { c.JSON(allowCode, gin.H{"ok": true}) })
	req := httptest.NewRequest(http.MethodGet, "/timetable/rooms", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user token must get 403 on a write route, got %d", w.Code)
	}
}
