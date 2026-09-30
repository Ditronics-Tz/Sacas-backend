package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/services"
)

// superAdminFixture wires the platform user controller with in-memory stubs.
type superAdminFixture struct {
	ctrl         *SuperAdminUserController
	users        *stubUserRepo
	institutions *stubInstitutionRepo
	audit        *recordingAuditRepo
	router       *gin.Engine
}

func newSuperAdminFixture(t *testing.T) *superAdminFixture {
	t.Helper()

	users := newStubUserRepo()
	institutions := newStubInstitutionRepo()
	audit := &recordingAuditRepo{}

	ctrl := NewSuperAdminUserController(users, institutions, nil, services.NewAuditRecorder(audit))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", float64(1))
		c.Set("role", string(models.RoleSuperAdmin))
		// Platform scope: no institution.
		c.Set("institution_id", uint(0))
		c.Next()
	})
	r.GET("/users", ctrl.ListUsers)
	r.GET("/users/:id", ctrl.GetUser)
	r.PUT("/users/:id", ctrl.UpdateUser)
	r.POST("/users/:id/status", ctrl.SetUserStatus)
	r.POST("/users/:id/reset-password", ctrl.ResetPassword)

	return &superAdminFixture{
		ctrl: ctrl, users: users, institutions: institutions,
		audit: audit, router: r,
	}
}

// TestListUsers_IncludesPlatformAccounts is a deliberate difference from the
// tenant listing: a platform operator must be able to see the super_admins,
// which the institution-scoped repository excludes.
func TestListUsers_IncludesPlatformAccounts(t *testing.T) {
	f := newSuperAdminFixture(t)
	f.users.seedUser(&models.User{
		ID: 1, Email: "root@x.com", FirstName: "R", LastName: "Oot",
		Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil,
	})
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "member@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users?limit=50", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !contains(body, "root@x.com") {
		t.Errorf("the platform account is missing: %s", body)
	}
	if !contains(body, "member@x.com") {
		t.Errorf("the institution member is missing: %s", body)
	}
}

// TestListUsers_FiltersByInstitutionAndStatus.
func TestListUsers_FiltersByInstitutionAndStatus(t *testing.T) {
	f := newSuperAdminFixture(t)
	instA, instB := tenantA, tenantB
	f.users.seedUser(&models.User{
		ID: 5, Email: "a@x.com", FirstName: "A", LastName: "A",
		Role: models.RoleUser, IsActive: true, InstitutionID: &instA,
	})
	f.users.seedUser(&models.User{
		ID: 6, Email: "b@x.com", FirstName: "B", LastName: "B",
		Role: models.RoleUser, IsActive: true, InstitutionID: &instB,
	})
	f.users.seedUser(&models.User{
		ID: 7, Email: "inactive@x.com", FirstName: "I", LastName: "Nactive",
		Role: models.RoleUser, IsActive: false, InstitutionID: &instA,
	})

	t.Run("by institution", func(t *testing.T) {
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users?institution_id=1&limit=50", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if contains(w.Body.String(), "b@x.com") {
			t.Errorf("institution filter leaked another tenant: %s", w.Body.String())
		}
		if !contains(w.Body.String(), "a@x.com") {
			t.Errorf("institution filter dropped the right tenant: %s", w.Body.String())
		}
	})

	t.Run("by status", func(t *testing.T) {
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users?is_active=false&limit=50", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !contains(w.Body.String(), "inactive@x.com") {
			t.Errorf("status filter missed the inactive account: %s", w.Body.String())
		}
		if contains(w.Body.String(), `"email":"a@x.com"`) {
			t.Errorf("status filter included an active account: %s", w.Body.String())
		}
	})

	t.Run("invalid filter is a 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users?institution_id=abc", nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

// TestUpdateUser_CannotAssignSupportRole: the support role belongs to a
// short-lived token, never to a row.
func TestUpdateUser_CannotAssignSupportRole(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := putJSON(f.router, "/users/5", `{"role":"institution_support"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	after, _ := f.users.GetByID(5)
	if after.Role == models.RoleSupport {
		t.Fatal("the support role was persisted on a user row")
	}
}

// TestUpdateUser_SuperAdminMustNotBeBoundToAnInstitution.
func TestUpdateUser_SuperAdminMustNotBeBoundToAnInstitution(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := putJSON(f.router, "/users/5", `{"role":"super_admin"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestUpdateUser_MakePlatformClearsInstitution is the documented way to turn a
// tenant account into a platform one.
func TestUpdateUser_MakePlatformClearsInstitution(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := putJSON(f.router, "/users/5", `{"role":"super_admin","make_platform":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	after, _ := f.users.GetByID(5)
	if after.InstitutionID != nil {
		t.Errorf("expected a NULL institution, got %v", *after.InstitutionID)
	}
	if after.Role != models.RoleSuperAdmin {
		t.Errorf("expected super_admin, got %s", after.Role)
	}
	// Moving institutions is a distinct, separately audited event.
	if !f.audit.hasAction(models.AuditRoleChange) {
		t.Error("the role change was not audited")
	}
	foundMove := false
	for _, e := range f.audit.entries {
		if e.Action == models.AuditUserUpdate && contains(e.Detail, "move_institution") {
			foundMove = true
		}
	}
	if !foundMove {
		t.Error("the institution move was not recorded as its own event")
	}
}

// TestUpdateUser_RejectsUnknownInstitution, so an account cannot be bound to
// something that does not exist.
func TestUpdateUser_RejectsUnknownInstitution(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := putJSON(f.router, "/users/5", `{"institution_id":999}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestSetUserStatus_SuspendAndReactivate covers the dedicated endpoint, which is
// the one a support engineer reaches for under pressure.
func TestSetUserStatus_SuspendAndReactivate(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := postJSON(f.router, "/users/5/status", `{"is_active":false,"reason":"policy breach"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	after, _ := f.users.GetByID(5)
	if after.IsActive {
		t.Error("the account was not suspended")
	}
	if !f.audit.hasAction(models.AuditUserSuspend) {
		t.Error("the suspension was not audited")
	}
	// The reason must be in the trail, not just the response.
	foundReason := false
	for _, e := range f.audit.entries {
		if e.Action == models.AuditUserSuspend && contains(e.Detail, "policy breach") {
			foundReason = true
		}
	}
	if !foundReason {
		t.Error("the suspension reason was not recorded")
	}

	w = postJSON(f.router, "/users/5/status", `{"is_active":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on reactivate, got %d", w.Code)
	}
	after, _ = f.users.GetByID(5)
	if !after.IsActive {
		t.Error("the account was not reactivated")
	}
	if !f.audit.hasAction(models.AuditUserActivate) {
		t.Error("the activation was not audited")
	}
}

// TestSetUserStatus_ProtectsLastPlatformAdmin stops a mistake that would need
// database access to undo.
func TestSetUserStatus_ProtectsLastPlatformAdmin(t *testing.T) {
	f := newSuperAdminFixture(t)
	f.users.seedUser(&models.User{
		ID: 1, Email: "root@x.com", FirstName: "R", LastName: "Oot",
		Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil,
	})

	w := postJSON(f.router, "/users/1/status", `{"is_active":false}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 suspending the last platform admin, got %d body=%s", w.Code, w.Body.String())
	}
	after, _ := f.users.GetByID(1)
	if !after.IsActive {
		t.Fatal("the last platform admin was suspended")
	}
}

// TestSetUserStatus_AllowsSuspendingOneOfSeveralPlatformAdmins.
func TestSetUserStatus_AllowsSuspendingOneOfSeveralPlatformAdmins(t *testing.T) {
	f := newSuperAdminFixture(t)
	f.users.seedUser(&models.User{
		ID: 1, Email: "root@x.com", FirstName: "R", LastName: "Oot",
		Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil,
	})
	f.users.seedUser(&models.User{
		ID: 2, Email: "root2@x.com", FirstName: "R", LastName: "Oot2",
		Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil,
	})

	w := postJSON(f.router, "/users/2/status", `{"is_active":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestSetUserStatus_SuperAdminBoundToInstitutionDoesNotCountAsPlatform: a
// super_admin with an institution is a tenant user and must not satisfy the
// "another platform admin exists" check.
func TestSetUserStatus_SuperAdminBoundToInstitutionDoesNotCountAsPlatform(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 1, Email: "root@x.com", FirstName: "R", LastName: "Oot",
		Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil,
	})
	f.users.seedUser(&models.User{
		ID: 2, Email: "tied@x.com", FirstName: "T", LastName: "Ied",
		Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: &inst,
	})

	// User 2 is not a platform account, so suspending user 1 is still blocked.
	w := postJSON(f.router, "/users/1/status", `{"is_active":false}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestResetPassword_GeneratedIsNotReturned is the important property: the
// generated password is emailed, never handed back to the operator.
func TestResetPassword_GeneratedIsNotReturned(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := postJSON(f.router, "/users/5/reset-password", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, leaked := body["password"]; leaked {
		t.Error("the response returned a password")
	}
	if generated, _ := body["generated"].(bool); !generated {
		t.Errorf("expected generated=true, got %v", body["generated"])
	}

	// The password must have changed, and the reset must be audited as high
	// severity.
	after, _ := f.users.GetByID(5)
	if after.Password == "" {
		t.Error("no password was stored")
	}
	if !f.audit.hasAction(models.AuditPasswordReset) {
		t.Error("the reset was not audited")
	}
	foundHigh := false
	for _, e := range f.audit.entries {
		if e.Action == models.AuditPasswordReset && contains(e.Detail, "high") {
			foundHigh = true
		}
	}
	if !foundHigh {
		t.Error("the reset was not marked high severity in the trail")
	}
}

// TestResetPassword_ExplicitIsAcceptedButRecorded: the break-glass path exists,
// and is distinguishable in the trail from a generated reset.
func TestResetPassword_ExplicitIsAcceptedButRecorded(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := postJSON(f.router, "/users/5/reset-password", `{"new_password":"Br0kenGlass!2026"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if generated, _ := body["generated"].(bool); generated {
		t.Error("expected generated=false for an explicit password")
	}
	foundFlag := false
	for _, e := range f.audit.entries {
		if e.Action == models.AuditPasswordReset && contains(e.Detail, "password_in_body") {
			foundFlag = true
		}
	}
	if !foundFlag {
		t.Error("an explicitly supplied password must be flagged in the trail")
	}
}

// TestResetPassword_RejectsWeakExplicitPassword.
func TestResetPassword_RejectsWeakExplicitPassword(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantA
	f.users.seedUser(&models.User{
		ID: 5, Email: "m@x.com", FirstName: "M", LastName: "Ember",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := postJSON(f.router, "/users/5/reset-password", `{"new_password":"weak"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a weak password, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestGetUser_PlatformCanReadAnyUser is the documented contrast with the tenant
// member route, which returns 404 for another tenant's account. The platform
// view is deliberately able to read any account.
func TestGetUser_PlatformCanReadAnyUser(t *testing.T) {
	f := newSuperAdminFixture(t)
	inst := tenantB
	f.users.seedUser(&models.User{
		ID: 6, Email: "other@x.com", FirstName: "O", LastName: "Ther",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/6", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), "other@x.com") {
		t.Errorf("body: %s", w.Body.String())
	}
}
