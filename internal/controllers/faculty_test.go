package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
)

// stubFacultyRepo is a minimal in-memory faculty repository for unit tests.
type stubFacultyRepo struct {
	items  map[uint]*models.Faculty
	nextID uint
}

func newStubFacultyRepo() *stubFacultyRepo {
	return &stubFacultyRepo{items: map[uint]*models.Faculty{}, nextID: 1}
}

func (r *stubFacultyRepo) Create(f *models.Faculty) error {
	f.ID = r.nextID
	r.nextID++
	cp := *f
	r.items[f.ID] = &cp
	return nil
}
func (r *stubFacultyRepo) GetByID(id uint) (*models.Faculty, error) {
	f, ok := r.items[id]
	if !ok {
		return nil, errNotFound
	}
	cp := *f
	return &cp, nil
}
func (r *stubFacultyRepo) Update(f *models.Faculty) error {
	if _, ok := r.items[f.ID]; !ok {
		return errNotFound
	}
	cp := *f
	r.items[f.ID] = &cp
	return nil
}
func (r *stubFacultyRepo) Delete(id uint) error {
	delete(r.items, id)
	return nil
}
func (r *stubFacultyRepo) GetAll(limit, offset int) ([]models.Faculty, error) {
	var out []models.Faculty
	for _, f := range r.items {
		out = append(out, *f)
	}
	return out, nil
}
func (r *stubFacultyRepo) GetWithCourses(id uint) (*models.Faculty, error) {
	return r.GetByID(id)
}

var errNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "not found" }

func init() {
	gin.SetMode(gin.TestMode)
}

func TestCreateFaculty_HappyPath(t *testing.T) {
	repo := newStubFacultyRepo()
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.POST("/faculties", ctrl.CreateFaculty)

	body := `{"name":"Engineering","description":"Eng faculty","hod_name":"Dr X"}`
	req := httptest.NewRequest(http.MethodPost, "/faculties", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] == nil {
		t.Error("expected message in response")
	}
}

func TestCreateFaculty_ValidationError(t *testing.T) {
	repo := newStubFacultyRepo()
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.POST("/faculties", ctrl.CreateFaculty)

	body := `{"name":"A"}` // too short
	req := httptest.NewRequest(http.MethodPost, "/faculties", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetFaculty_NotFound(t *testing.T) {
	repo := newStubFacultyRepo()
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.GET("/faculties/:id", ctrl.GetFaculty)

	req := httptest.NewRequest(http.MethodGet, "/faculties/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateFaculty_HappyPath(t *testing.T) {
	repo := newStubFacultyRepo()
	_ = repo.Create(&models.Faculty{Name: "Old"})
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.PUT("/faculties/:id", ctrl.UpdateFaculty)

	body := `{"name":"New Name","hod_email":"hod@example.com"}`
	req := httptest.NewRequest(http.MethodPut, "/faculties/1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}
