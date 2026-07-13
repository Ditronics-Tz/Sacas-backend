package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
)

type stubStaffRepo struct {
	staff   map[uint]*models.Staff
	modules map[uint][]models.Module
}

func newStubStaffRepo() *stubStaffRepo {
	return &stubStaffRepo{
		staff:   map[uint]*models.Staff{1: {ID: 1, Name: "Dr A", Email: "a@x.com", FacultyID: 1}},
		modules: map[uint][]models.Module{},
	}
}

func (r *stubStaffRepo) Create(s *models.Staff) error                       { return nil }
func (r *stubStaffRepo) GetByID(id uint) (*models.Staff, error) {
	s, ok := r.staff[id]
	if !ok {
		return nil, errNotFound
	}
	cp := *s
	return &cp, nil
}
func (r *stubStaffRepo) GetByEmail(email string) (*models.Staff, error)     { return nil, errNotFound }
func (r *stubStaffRepo) Update(s *models.Staff) error                       { return nil }
func (r *stubStaffRepo) Delete(id uint) error                               { return nil }
func (r *stubStaffRepo) GetAll(limit, offset int) ([]models.Staff, error)   { return nil, nil }
func (r *stubStaffRepo) GetByFaculty(facultyID uint, limit, offset int) ([]models.Staff, error) {
	return nil, nil
}
func (r *stubStaffRepo) GetWithModules(id uint) (*models.Staff, error)      { return r.GetByID(id) }
func (r *stubStaffRepo) UpdatePreferences(id uint, preferences string) error { return nil }
func (r *stubStaffRepo) AssignModule(staffID, moduleID uint) error {
	r.modules[staffID] = append(r.modules[staffID], models.Module{ID: moduleID})
	return nil
}
func (r *stubStaffRepo) UnassignModule(staffID, moduleID uint) error {
	list := r.modules[staffID]
	var next []models.Module
	for _, m := range list {
		if m.ID != moduleID {
			next = append(next, m)
		}
	}
	r.modules[staffID] = next
	return nil
}
func (r *stubStaffRepo) ListModules(staffID uint) ([]models.Module, error) {
	return r.modules[staffID], nil
}
func (r *stubStaffRepo) ListStaffForModule(moduleID uint) ([]models.Staff, error) {
	return nil, nil
}

type stubModuleRepo struct {
	items map[uint]*models.Module
}

func newStubModuleRepo() *stubModuleRepo {
	return &stubModuleRepo{items: map[uint]*models.Module{2: {ID: 2, Name: "Algo"}}}
}

func (r *stubModuleRepo) Create(m *models.Module) error { return nil }
func (r *stubModuleRepo) GetByID(id uint) (*models.Module, error) {
	m, ok := r.items[id]
	if !ok {
		return nil, errNotFound
	}
	cp := *m
	return &cp, nil
}
func (r *stubModuleRepo) Update(m *models.Module) error { return nil }
func (r *stubModuleRepo) Delete(id uint) error          { return nil }
func (r *stubModuleRepo) GetAll(limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (r *stubModuleRepo) GetByCourse(courseID uint, limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (r *stubModuleRepo) GetByType(moduleType models.ModuleType, limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (r *stubModuleRepo) GetGeneralModules(limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (r *stubModuleRepo) GetWithStaff(id uint) (*models.Module, error) { return r.GetByID(id) }

func TestAssignModule_HappyPath(t *testing.T) {
	staffRepo := newStubStaffRepo()
	modRepo := newStubModuleRepo()
	ctrl := NewStaffController(staffRepo, modRepo)
	r := gin.New()
	r.POST("/staff/:staff_id/modules/:module_id", ctrl.AssignModule)

	req := httptest.NewRequest(http.MethodPost, "/staff/1/modules/2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(staffRepo.modules[1]) != 1 || staffRepo.modules[1][0].ID != 2 {
		t.Fatalf("module not assigned: %+v", staffRepo.modules)
	}
}

func TestAssignModule_StaffNotFound(t *testing.T) {
	ctrl := NewStaffController(newStubStaffRepo(), newStubModuleRepo())
	r := gin.New()
	r.POST("/staff/:staff_id/modules/:module_id", ctrl.AssignModule)

	req := httptest.NewRequest(http.MethodPost, "/staff/99/modules/2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAssignModule_ModuleNotFound(t *testing.T) {
	ctrl := NewStaffController(newStubStaffRepo(), newStubModuleRepo())
	r := gin.New()
	r.POST("/staff/:staff_id/modules/:module_id", ctrl.AssignModule)

	req := httptest.NewRequest(http.MethodPost, "/staff/1/modules/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUnassignModule_HappyPath(t *testing.T) {
	staffRepo := newStubStaffRepo()
	_ = staffRepo.AssignModule(1, 2)
	ctrl := NewStaffController(staffRepo, newStubModuleRepo())
	r := gin.New()
	r.DELETE("/staff/:staff_id/modules/:module_id", ctrl.UnassignModule)

	req := httptest.NewRequest(http.MethodDelete, "/staff/1/modules/2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(staffRepo.modules[1]) != 0 {
		t.Fatalf("expected empty modules after unassign")
	}
}
