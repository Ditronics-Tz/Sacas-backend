package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
)

// recordingAuditRepo captures audit entries for assertions.
type recordingAuditRepo struct {
	entries []*models.AuditLog
}

func (r *recordingAuditRepo) Create(entry *models.AuditLog) error {
	r.entries = append(r.entries, entry)
	return nil
}
func (r *recordingAuditRepo) List(repositories.AuditLogFilter, int, int) ([]models.AuditLog, error) {
	return nil, nil
}
func (r *recordingAuditRepo) Count(repositories.AuditLogFilter) (int64, error) { return 0, nil }
func (r *recordingAuditRepo) ListForTarget(*uint, string, string, int) ([]models.AuditLog, error) {
	return nil, nil
}

func (r *recordingAuditRepo) hasAction(a models.AuditAction) bool {
	for _, e := range r.entries {
		if e.Action == a {
			return true
		}
	}
	return false
}

// asActor routes a request with the caller's identity and tenant scope
// resolved, the way TenantMiddleware would.
func asActor(role models.UserRole, instID uint, user *models.User) gin.HandlerFunc {
	return func(c *gin.Context) {
		if user != nil {
			c.Set("user", user)
		}
		c.Set("user_id", float64(1))
		c.Set("role", string(role))
		c.Set("email", "actor@x.com")
		c.Set("institution_id", instID)
		c.Next()
	}
}

// newRoleTestRouter builds a router whose requests carry the caller's identity
// and tenant scope, the way TenantMiddleware would.
//
// The identity middleware is installed BEFORE the routes are registered:
// Gin's Use only applies to routes added afterwards.
func newRoleTestRouter(t *testing.T, repo repositories.UserRepository, audit *recordingAuditRepo,
	role models.UserRole, instID uint, user *models.User, register func(r *gin.Engine, ctrl *UserController),
) *gin.Engine {
	t.Helper()
	ctrl := NewUserController(repo, services.NewAuditRecorder(audit))
	r := gin.New()
	r.Use(asActor(role, instID, user))
	register(r, ctrl)
	return r
}

func validCreateBody(email, role string) string {
	body := `{"email":"` + email + `","password":"Str0ngPass!","first_name":"Test","last_name":"User","role":"` + role + `"}`
	return body
}

func postJSON(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func putJSON(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestCreateUser_InstitutionAdminMayOnlyCreateLecturers is the anti-escalation
// rule: a tenant admin must not be able to mint a peer admin or a coordinator.
func TestCreateUser_InstitutionAdminMayOnlyCreateLecturers(t *testing.T) {
	forbidden := []models.UserRole{
		models.RoleAdmin,
		models.RoleAcademicCoord,
		models.RoleExamCoord,
		models.RoleSuperAdmin,
		models.RoleSupport,
	}
	for _, role := range forbidden {
		t.Run("cannot create "+string(role), func(t *testing.T) {
			repo := newStubUserRepo()
			audit := &recordingAuditRepo{}
			inst := tenantA
			admin := &models.User{ID: 1, Email: "admin@x.com", Role: models.RoleAdmin, InstitutionID: &inst}

			r := newRoleTestRouter(t, repo, audit, models.RoleAdmin, tenantA, admin,
				func(r *gin.Engine, ctrl *UserController) {
					r.POST("/users", ctrl.CreateUser)
				})

			w := postJSON(r, "/users", validCreateBody("target@x.com", string(role)))
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 creating %s, got %d body=%s", role, w.Code, w.Body.String())
			}
			if _, err := repo.GetByEmail("target@x.com"); err == nil {
				t.Fatal("the user was created despite the refusal")
			}
			// A refused privilege escalation is exactly what the trail is for.
			if !audit.hasAction(models.AuditUserCreate) {
				t.Error("expected the refusal to be audited")
			}
		})
	}

	t.Run("can create a lecturer", func(t *testing.T) {
		repo := newStubUserRepo()
		audit := &recordingAuditRepo{}
		inst := tenantA
		admin := &models.User{ID: 1, Email: "admin@x.com", Role: models.RoleAdmin, InstitutionID: &inst}

		r := newRoleTestRouter(t, repo, audit, models.RoleAdmin, tenantA, admin,
			func(r *gin.Engine, ctrl *UserController) {
				r.POST("/users", ctrl.CreateUser)
			})

		w := postJSON(r, "/users", validCreateBody("lecturer@x.com", string(models.RoleUser)))
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
		}
		if !audit.hasAction(models.AuditUserCreate) {
			t.Error("expected the creation to be audited")
		}
	})
}

// TestCreateUser_PlatformSuperAdminCreatesInstitutionAdmins covers the other
// half of the policy: the platform is how an institution admin comes to exist.
func TestCreateUser_PlatformSuperAdminCreatesInstitutionAdmins(t *testing.T) {
	repo := newStubUserRepo()
	audit := &recordingAuditRepo{}
	root := &models.User{ID: 1, Email: "root@x.com", Role: models.RoleSuperAdmin, InstitutionID: nil}

	r := newRoleTestRouter(t, repo, audit, models.RoleSuperAdmin, 0, root,
		func(r *gin.Engine, ctrl *UserController) {
			r.POST("/users", ctrl.CreateUser)
		})

	body := `{"email":"inst-admin@x.com","password":"Str0ngPass!","first_name":"Inst","last_name":"Admin","role":"administrator","institution_id":` +
		itoa(tenantA) + `}`
	w := postJSON(r, "/users", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	created, err := repo.GetByEmail("inst-admin@x.com")
	if err != nil {
		t.Fatalf("expected the admin to be created: %v", err)
	}
	if created.Role != models.RoleAdmin {
		t.Errorf("expected role administrator, got %s", created.Role)
	}
	// The named institution must be honoured for a platform caller.
	if created.InstitutionID == nil || *created.InstitutionID != tenantA {
		t.Errorf("expected institution %d, got %v", tenantA, created.InstitutionID)
	}
}

// TestCreateUser_SuperAdminMustNotBeBoundToAnInstitution rejects the combination
// that the workspace gate would refuse anyway, with a clearer error at write
// time.
func TestCreateUser_SuperAdminMustNotBeBoundToAnInstitution(t *testing.T) {
	repo := newStubUserRepo()
	audit := &recordingAuditRepo{}
	root := &models.User{ID: 1, Email: "root@x.com", Role: models.RoleSuperAdmin, InstitutionID: nil}

	r := newRoleTestRouter(t, repo, audit, models.RoleSuperAdmin, 0, root,
		func(r *gin.Engine, ctrl *UserController) {
			r.POST("/users", ctrl.CreateUser)
		})

	body := `{"email":"tied@x.com","password":"Str0ngPass!","first_name":"Tied","last_name":"Admin","role":"super_admin","institution_id":` +
		itoa(tenantA) + `}`
	w := postJSON(r, "/users", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a super_admin bound to an institution, got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := repo.GetByEmail("tied@x.com"); err == nil {
		t.Fatal("the account was created despite the refusal")
	}
}

// TestUpdateUser_RoleChangeIsAuditedWithBeforeAndAfter is the trail property
// that answers "who was promoted to what, and when".
func TestUpdateUser_RoleChangeIsAuditedWithBeforeAndAfter(t *testing.T) {
	repo := newStubUserRepo()
	audit := &recordingAuditRepo{}
	inst := tenantB
	repo.seedUser(&models.User{
		ID: 5, Email: "coordinator@x.com", FirstName: "Co", LastName: "Ord",
		Role: models.RoleAcademicCoord, IsActive: true, InstitutionID: &inst,
	})

	// Acting as a platform super_admin so the role change is permitted.
	root := &models.User{ID: 1, Email: "root@x.com", Role: models.RoleSuperAdmin, InstitutionID: nil}
	r := newRoleTestRouter(t, repo, audit, models.RoleSuperAdmin, 0, root,
		func(r *gin.Engine, ctrl *UserController) {
			r.PUT("/users/:id", ctrl.UpdateUser)
		})

	w := putJSON(r, "/users/5", `{"role":"administrator"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var roleChange *models.AuditLog
	for _, e := range audit.entries {
		if e.Action == models.AuditRoleChange {
			roleChange = e
			break
		}
	}
	if roleChange == nil {
		t.Fatal("expected a role.change audit entry")
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(roleChange.Detail), &detail); err != nil {
		t.Fatalf("detail is not valid JSON: %v", err)
	}
	if detail["from"] != string(models.RoleAcademicCoord) {
		t.Errorf("expected from=%s, got %v", models.RoleAcademicCoord, detail["from"])
	}
	if detail["to"] != string(models.RoleAdmin) {
		t.Errorf("expected to=%s, got %v", models.RoleAdmin, detail["to"])
	}
}

// TestUpdateUser_InstitutionAdminCannotPromote is the escalation guard on the
// update path as well as create.
func TestUpdateUser_InstitutionAdminCannotPromote(t *testing.T) {
	repo := newStubUserRepo()
	audit := &recordingAuditRepo{}
	inst := tenantA
	repo.seedUser(&models.User{
		ID: 5, Email: "lecturer@x.com", FirstName: "Lee", LastName: "Ctur",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})
	admin := &models.User{ID: 1, Email: "admin@x.com", Role: models.RoleAdmin, InstitutionID: &inst}

	r := newRoleTestRouter(t, repo, audit, models.RoleAdmin, tenantA, admin,
		func(r *gin.Engine, ctrl *UserController) {
			r.PUT("/users/:id", ctrl.UpdateUser)
		})

	w := putJSON(r, "/users/5", `{"role":"administrator"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}

	after, _ := repo.GetByID(5)
	if after.Role != models.RoleUser {
		t.Fatalf("the role was changed despite the refusal: %s", after.Role)
	}
}

// TestUpdateUser_SuspensionIsAuditedSeparately covers the "who was
// re-enabled" question.
func TestUpdateUser_SuspensionIsAuditedSeparately(t *testing.T) {
	repo := newStubUserRepo()
	audit := &recordingAuditRepo{}
	inst := tenantA
	repo.seedUser(&models.User{
		ID: 5, Email: "lecturer@x.com", FirstName: "Lee", LastName: "Ctur",
		Role: models.RoleUser, IsActive: true, InstitutionID: &inst,
	})
	admin := &models.User{ID: 1, Email: "admin@x.com", Role: models.RoleAdmin, InstitutionID: &inst}

	r := newRoleTestRouter(t, repo, audit, models.RoleAdmin, tenantA, admin,
		func(r *gin.Engine, ctrl *UserController) {
			r.PUT("/users/:id", ctrl.UpdateUser)
		})

	w := putJSON(r, "/users/5", `{"is_active":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !audit.hasAction(models.AuditUserSuspend) {
		t.Error("expected a user.suspend audit entry")
	}

	// Reactivating is a distinct event.
	audit.entries = nil
	w = putJSON(r, "/users/5", `{"is_active":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !audit.hasAction(models.AuditUserActivate) {
		t.Error("expected a user.activate audit entry")
	}
}

// TestGetMyPermissions_ShapesByRole is the /me/permissions contract the frontend
// depends on.
func TestGetMyPermissions_ShapesByRole(t *testing.T) {
	cases := []struct {
		name          string
		role          models.UserRole
		instID        uint
		wantWorkspace string
		wantPlatform  bool
		wantReadOnly  bool
	}{
		{"platform super_admin", models.RoleSuperAdmin, 0, "platform", true, false},
		{"institution admin", models.RoleAdmin, tenantA, "institution", false, false},
		{"academic coordinator", models.RoleAcademicCoord, tenantA, "institution", false, false},
		// The exam coordinator cannot edit the curriculum, but it does write
		// exams, so it is not read-only overall.
		{"exam coordinator", models.RoleExamCoord, tenantA, "institution", false, false},
		{"lecturer", models.RoleUser, tenantA, "institution", false, true},
		{"support session", models.RoleSupport, tenantA, "support", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newStubUserRepo()
			ctrl := NewUserController(repo, nil)
			r := gin.New()
			r.GET("/me/permissions", asActor(tc.role, tc.instID, nil), ctrl.GetMyPermissions)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me/permissions", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			var body struct {
				Role              string   `json:"role"`
				Permissions       []string `json:"permissions"`
				AssignableRoles   []string `json:"assignable_roles"`
				Workspace         string   `json:"workspace"`
				IsPlatformAccount bool     `json:"is_platform_account"`
				IsSupportSession  bool     `json:"is_support_session"`
				ReadOnly          bool     `json:"read_only"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			if body.Role != string(tc.role) {
				t.Errorf("role: got %s want %s", body.Role, tc.role)
			}
			if body.Workspace != tc.wantWorkspace {
				t.Errorf("workspace: got %s want %s", body.Workspace, tc.wantWorkspace)
			}
			if body.IsPlatformAccount != tc.wantPlatform {
				t.Errorf("is_platform_account: got %v want %v", body.IsPlatformAccount, tc.wantPlatform)
			}
			if body.ReadOnly != tc.wantReadOnly {
				t.Errorf("read_only: got %v want %v", body.ReadOnly, tc.wantReadOnly)
			}
			// The advertised permission list must match the server's own map,
			// or the UI and the server disagree about what the role can do.
			if len(body.Permissions) != len(auth.PermissionsFor(tc.role)) {
				t.Errorf("permission count: got %d want %d",
					len(body.Permissions), len(auth.PermissionsFor(tc.role)))
			}
		})
	}
}

// TestGetMyPermissions_SupportSessionIsReadOnly asserts the response tells the
// UI to disable editing, rather than relying on the client to hardcode it.
func TestGetMyPermissions_SupportSessionIsReadOnly(t *testing.T) {
	repo := newStubUserRepo()
	ctrl := NewUserController(repo, nil)
	r := gin.New()
	r.GET("/me/permissions", asActor(models.RoleSupport, tenantA, nil), ctrl.GetMyPermissions)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me/permissions", nil))

	var body struct {
		ReadOnly         bool     `json:"read_only"`
		IsSupportSession bool     `json:"is_support_session"`
		Permissions      []string `json:"permissions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !body.ReadOnly || !body.IsSupportSession {
		t.Errorf("expected a read-only support session, got %+v", body)
	}
	for _, p := range body.Permissions {
		if !auth.HasPermission(models.RoleSupport, auth.Permission(p)) {
			t.Errorf("advertised permission %s is not held by the support role", p)
		}
	}
}

// TestGetMyPermissions_AssignableRolesReflectsCaller guards the anti-escalation
// list the UI uses to populate a role dropdown.
func TestGetMyPermissions_AssignableRolesReflectsCaller(t *testing.T) {
	cases := map[models.UserRole][]string{
		models.RoleAdmin:         {string(models.RoleUser)},
		models.RoleAcademicCoord: {string(models.RoleUser)},
		models.RoleUser:          {string(models.RoleUser)},
		models.RoleSuperAdmin: {
			string(models.RoleSuperAdmin), string(models.RoleAdmin),
			string(models.RoleAcademicCoord), string(models.RoleExamCoord),
			string(models.RoleUser),
		},
	}
	for role, want := range cases {
		t.Run(string(role), func(t *testing.T) {
			repo := newStubUserRepo()
			ctrl := NewUserController(repo, nil)
			r := gin.New()
			r.GET("/me/permissions", asActor(role, tenantA, nil), ctrl.GetMyPermissions)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me/permissions", nil))

			var body struct {
				AssignableRoles []string `json:"assignable_roles"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if len(body.AssignableRoles) != len(want) {
				t.Fatalf("got %v want %v", body.AssignableRoles, want)
			}
			for _, role := range want {
				found := false
				for _, got := range body.AssignableRoles {
					if got == role {
						found = true
					}
				}
				if !found {
					t.Errorf("missing assignable role %s in %v", role, body.AssignableRoles)
				}
			}
			// The support role must never appear as assignable.
			for _, got := range body.AssignableRoles {
				if got == string(models.RoleSupport) {
					t.Error("the support role must not be assignable")
				}
			}
		})
	}
}

func itoa(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}
