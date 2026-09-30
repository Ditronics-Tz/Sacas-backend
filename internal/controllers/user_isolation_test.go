package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// stubUserRepo is a tenant-aware user repository. It mirrors the real one:
// GetByID and GetByEmail are unscoped (they back authentication, where the
// tenant is not yet known), while listing, updating, and deleting are scoped.
type stubUserRepo struct {
	users  map[uint]*models.User
	nextID uint
}

func newStubUserRepo() *stubUserRepo {
	return &stubUserRepo{users: map[uint]*models.User{}, nextID: 1}
}

// seedUser inserts a user directly, bypassing the tenant checks.
func (r *stubUserRepo) seedUser(u *models.User) *models.User {
	if u.ID == 0 {
		u.ID = r.nextID
	}
	if u.ID >= r.nextID {
		r.nextID = u.ID + 1
	}
	cp := *u
	r.users[u.ID] = &cp
	return &cp
}

func (r *stubUserRepo) Create(user *models.User) error {
	r.seedUser(user)
	return nil
}

func (r *stubUserRepo) GetByID(id uint) (*models.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *stubUserRepo) GetByEmail(email string) (*models.User, error) {
	for _, u := range r.users {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// recordScope maps a user's institution to the scope value the repository
// layer uses, so the stub matches repositories.TenantScope: a NULL
// institution (a platform account) is visible only to a platform caller.
func recordScope(u *models.User) uint {
	if u.InstitutionID == nil {
		return repositories.PlatformScope
	}
	return *u.InstitutionID
}

func (r *stubUserRepo) Update(institutionID uint, user *models.User) error {
	existing, ok := r.users[user.ID]
	if !ok || !repositories.InTenant(recordScope(existing), institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *user
	// The tenant must not be changeable through a profile update.
	cp.InstitutionID = existing.InstitutionID
	r.users[user.ID] = &cp
	return nil
}

func (r *stubUserRepo) UpdateSelf(user *models.User) error {
	scope := repositories.PlatformScope
	if user.InstitutionID != nil {
		scope = *user.InstitutionID
	}
	return r.Update(scope, user)
}

// SetInstitution moves the account's institution, mirroring the real repository:
// it is the ONLY way the institution changes, and a profile update cannot do it.
func (r *stubUserRepo) SetInstitution(id uint, institutionID *uint) error {
	u, ok := r.users[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	cp := *u
	if institutionID == nil {
		cp.InstitutionID = nil
	} else {
		inst := *institutionID
		cp.InstitutionID = &inst
	}
	r.users[id] = &cp
	return nil
}

func (r *stubUserRepo) Delete(institutionID, id uint) error {
	u, ok := r.users[id]
	if !ok || !repositories.InTenant(recordScope(u), institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.users, id)
	return nil
}

func (r *stubUserRepo) GetAll(institutionID uint, limit, offset int) ([]models.User, error) {
	var out []models.User
	for _, u := range r.users {
		if u.InstitutionID == nil {
			// Platform super_admins belong to no institution; a tenant-scoped
			// listing must never include them.
			if !repositories.IsPlatformScope(institutionID) {
				continue
			}
		} else if !repositories.InTenant(*u.InstitutionID, institutionID) {
			continue
		}
		out = append(out, *u)
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubUserRepo) GetByRole(institutionID uint, role string, limit, offset int) ([]models.User, error) {
	all, _ := r.GetAll(institutionID, 0, 0)
	var out []models.User
	for _, u := range all {
		if u.Role == models.UserRole(role) {
			out = append(out, u)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubUserRepo) CountByInstitution(institutionID uint) (int64, error) {
	users, _ := r.GetAll(institutionID, 0, 0)
	return int64(len(users)), nil
}

func (r *stubUserRepo) UpdatePassword(id uint, hashed string) error {
	u, ok := r.users[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	cp := *u
	cp.Password = hashed
	r.users[id] = &cp
	return nil
}

func (r *stubUserRepo) UpdateRole(institutionID, id uint, role string) error {
	u, ok := r.users[id]
	if !ok || !repositories.InTenant(recordScope(u), institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *u
	cp.Role = models.UserRole(role)
	r.users[id] = &cp
	return nil
}

func (r *stubUserRepo) ActivateUser(institutionID, id uint) error {
	return r.setActive(institutionID, id, true)
}

func (r *stubUserRepo) DeactivateUser(institutionID, id uint) error {
	return r.setActive(institutionID, id, false)
}

func (r *stubUserRepo) setActive(institutionID, id uint, active bool) error {
	u, ok := r.users[id]
	if !ok || !repositories.InTenant(recordScope(u), institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *u
	cp.IsActive = active
	r.users[id] = &cp
	return nil
}

func TestUser_CrossTenantIsolation(t *testing.T) {
	repo := newStubUserRepo()
	instA, instB := tenantA, tenantB
	repo.seedUser(&models.User{
		ID: 1, Email: "a-admin@x.com", FirstName: "A", LastName: "Admin",
		Role: models.RoleAdmin, IsActive: true, InstitutionID: &instA,
	})
	repo.seedUser(&models.User{
		ID: 2, Email: "b-admin@x.com", FirstName: "B", LastName: "Admin",
		Role: models.RoleAdmin, IsActive: true, InstitutionID: &instB,
	})
	// A platform super_admin has no institution and must not appear in a
	// tenant's user listing.
	repo.seedUser(&models.User{
		ID: 3, Email: "root@x.com", FirstName: "Root", LastName: "Admin",
		Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil,
	})

	ctrl := NewUserController(repo, nil)
	r := isolationRouter(t, func(r *gin.Engine) {
		r.GET("/users", withTenant(ctrl.GetUsers, tenantA))
		r.GET("/users/:id", withTenant(ctrl.GetUser, tenantA))
		r.PUT("/users/:id", withTenant(ctrl.UpdateUser, tenantA))
		r.DELETE("/users/:id", withTenant(ctrl.DeleteUser, tenantA))
		r.POST("/users", withTenant(ctrl.CreateUser, tenantA))
	})

	t.Run("list only returns own tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users?limit=50", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if contains(body, "b-admin@x.com") {
			t.Fatalf("list leaked another tenant's user: %s", body)
		}
		if contains(body, "root@x.com") {
			t.Fatalf("list leaked a platform super_admin to a tenant: %s", body)
		}
		if !contains(body, "a-admin@x.com") {
			t.Fatalf("list missing own user: %s", body)
		}
	})

	t.Run("get other tenant is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if contains(w.Body.String(), "b-admin@x.com") {
			t.Fatalf("response leaked another tenant's user: %s", w.Body.String())
		}
	})

	t.Run("get platform super_admin is 404 for a tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/3", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for a platform account, got %d", w.Code)
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/users/2", bytes.NewBufferString(`{"first_name":"Hijacked"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if repo.users[2].FirstName != "B" {
			t.Fatalf("cross-tenant update mutated the record: %q", repo.users[2].FirstName)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if _, ok := repo.users[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})

	t.Run("cannot create a user in another institution", func(t *testing.T) {
		body := `{"email":"sneaky@x.com","password":"Str0ngPass!","first_name":"Sneaky","last_name":"User","role":"user","institution_id":2}`
		req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
		}
		// The institution_id in the payload must have been ignored: the new
		// user belongs to the caller's own institution.
		created, err := repo.GetByEmail("sneaky@x.com")
		if err != nil {
			t.Fatalf("expected the user to be created: %v", err)
		}
		if created.InstitutionID == nil || *created.InstitutionID != tenantA {
			t.Fatalf("user created into institution %v, want %d", created.InstitutionID, tenantA)
		}
	})
}
