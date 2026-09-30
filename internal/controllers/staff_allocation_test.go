package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
)

// staffFixture builds a staff repo and module repo that share one institution.
func staffFixture() (*stubStaffRepo, *stubModuleRepo) {
	staffRepo := newStubStaffRepo()
	modRepo := newStubModuleRepo()
	staffRepo.seedStaff(&models.Staff{
		Name: "Dr A", Email: "a@x.com", FacultyID: 1,
		InstitutionID: tenantA, MaxHours: 40,
	})
	modRepo.seed(&models.Module{
		Name: "Algo", Type: models.ModuleTypeCore, CreditHours: 2,
		InstitutionID: tenantA,
	})
	return staffRepo, modRepo
}

func TestAssignModule_HappyPath(t *testing.T) {
	staffRepo, modRepo := staffFixture()
	ctrl := NewStaffController(staffRepo, modRepo)
	r := gin.New()
	r.POST("/staff/:id/modules/:module_id", withTenant(ctrl.AssignModule, tenantA))

	req := httptest.NewRequest(http.MethodPost, "/staff/1/modules/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(staffRepo.modules[1]) != 1 || staffRepo.modules[1][0].ID != 1 {
		t.Fatalf("module not assigned: %+v", staffRepo.modules)
	}
}

func TestAssignModule_StaffNotFound(t *testing.T) {
	staffRepo, modRepo := staffFixture()
	ctrl := NewStaffController(staffRepo, modRepo)
	r := gin.New()
	r.POST("/staff/:id/modules/:module_id", withTenant(ctrl.AssignModule, tenantA))

	req := httptest.NewRequest(http.MethodPost, "/staff/99/modules/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAssignModule_ModuleNotFound(t *testing.T) {
	staffRepo, modRepo := staffFixture()
	ctrl := NewStaffController(staffRepo, modRepo)
	r := gin.New()
	r.POST("/staff/:id/modules/:module_id", withTenant(ctrl.AssignModule, tenantA))

	req := httptest.NewRequest(http.MethodPost, "/staff/1/modules/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUnassignModule_HappyPath(t *testing.T) {
	staffRepo, modRepo := staffFixture()
	_ = staffRepo.AssignModule(tenantA, 1, 1)
	ctrl := NewStaffController(staffRepo, modRepo)
	r := gin.New()
	r.DELETE("/staff/:id/modules/:module_id", withTenant(ctrl.UnassignModule, tenantA))

	req := httptest.NewRequest(http.MethodDelete, "/staff/1/modules/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(staffRepo.modules[1]) != 0 {
		t.Fatalf("expected empty modules after unassign")
	}
}

// TestStaff_CrossTenantAllocation is the isolation test for the staff↔module
// association: a tenant must not be able to link its staff to another
// institution's module, nor read the staff allocated to one.
func TestStaff_CrossTenantAllocation(t *testing.T) {
	staffRepo, modRepo := staffFixture()
	// Module 2 belongs to institution B.
	modRepo.seed(&models.Module{
		Name: "B-Only Module", Type: models.ModuleTypeCore, CreditHours: 2,
		InstitutionID: tenantB,
	})
	// Staff 2 belongs to institution B and is allocated to module 2 there.
	staffRepo.seedStaff(&models.Staff{
		Name: "Dr B", Email: "b@x.com", FacultyID: 1,
		InstitutionID: tenantB, MaxHours: 40,
	})
	_ = staffRepo.AssignModule(tenantB, 2, 2)

	ctrl := NewStaffController(staffRepo, modRepo)
	r := gin.New()
	r.POST("/staff/:id/modules/:module_id", withTenant(ctrl.AssignModule, tenantA))
	r.GET("/modules/:id/staff", withTenant(ctrl.ListModuleStaff, tenantA))
	r.GET("/staff/:id/modules", withTenant(ctrl.ListStaffModules, tenantA))
	r.DELETE("/staff/:id/modules/:module_id", withTenant(ctrl.UnassignModule, tenantA))

	t.Run("cannot assign another tenant's module", func(t *testing.T) {
		w := httptest.NewRecorder()
		// Institution A's staff (ID 1) tries to claim B's module (ID 2).
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/staff/1/modules/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when assigning a cross-tenant module, got %d", w.Code)
		}
		if len(staffRepo.modules[1]) != 0 {
			t.Fatalf("cross-tenant assignment created a link: %+v", staffRepo.modules[1])
		}
	})

	t.Run("cannot list another tenant's module staff", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/modules/2/staff", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 listing a cross-tenant module, got %d", w.Code)
		}
		if contains(w.Body.String(), "Dr B") {
			t.Fatalf("response leaked another tenant's staff: %s", w.Body.String())
		}
	})

	t.Run("cannot read another tenant's staff modules", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/staff/2/modules", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 reading a cross-tenant staff record, got %d", w.Code)
		}
	})

	t.Run("cannot unassign another tenant's link", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/staff/2/modules/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 unassigning a cross-tenant link, got %d", w.Code)
		}
		if len(staffRepo.modules[2]) != 1 {
			t.Fatal("cross-tenant unassign removed the link")
		}
	})
}
