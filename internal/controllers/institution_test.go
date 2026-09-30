package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_boilerplate/internal/models"
)

// stubInstitutionRepo is an in-memory institution repository.
type stubInstitutionRepo struct {
	items  map[uint]*models.Institution
	nextID uint
}

func newStubInstitutionRepo() *stubInstitutionRepo {
	return &stubInstitutionRepo{items: map[uint]*models.Institution{}, nextID: 1}
}

func (r *stubInstitutionRepo) Create(inst *models.Institution) error {
	inst.ID = r.nextID
	r.nextID++
	cp := *inst
	r.items[inst.ID] = &cp
	return nil
}

func (r *stubInstitutionRepo) GetByID(id uint) (*models.Institution, error) {
	inst, ok := r.items[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *inst
	return &cp, nil
}

func (r *stubInstitutionRepo) GetBySlug(slug string) (*models.Institution, error) {
	for _, inst := range r.items {
		if equalFold(inst.Slug, slug) {
			cp := *inst
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *stubInstitutionRepo) Update(inst *models.Institution) error {
	if _, ok := r.items[inst.ID]; !ok {
		return gorm.ErrRecordNotFound
	}
	cp := *inst
	r.items[inst.ID] = &cp
	return nil
}

func (r *stubInstitutionRepo) Delete(id uint) error {
	if _, ok := r.items[id]; !ok {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubInstitutionRepo) GetAll(limit, offset int) ([]models.Institution, error) {
	var out []models.Institution
	for _, inst := range r.items {
		out = append(out, *inst)
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubInstitutionRepo) CountAll() (int64, error) {
	return int64(len(r.items)), nil
}

func (r *stubInstitutionRepo) GetByStatus(status models.InstitutionStatus, limit, offset int) ([]models.Institution, error) {
	var out []models.Institution
	for _, inst := range r.items {
		if inst.Status == status {
			out = append(out, *inst)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubInstitutionRepo) CountByStatus(status models.InstitutionStatus) (int64, error) {
	var n int64
	for _, inst := range r.items {
		if inst.Status == status {
			n++
		}
	}
	return n, nil
}

func (r *stubInstitutionRepo) CountUsers(id uint) (int64, error) { return 0, nil }

func TestCreateInstitution_DefaultsToPending(t *testing.T) {
	repo := newStubInstitutionRepo()
	ctrl := NewInstitutionController(repo, nil)
	r := gin.New()
	r.POST("/institutions", ctrl.Create)

	body := `{"name":"Dar es Salaam University","type":"university"}`
	req := httptest.NewRequest(http.MethodPost, "/institutions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	created, err := repo.GetBySlug("dar-es-salaam-university")
	if err != nil {
		t.Fatalf("expected the slug to be derived from the name: %v", err)
	}
	// A new institution must not be usable until a platform admin activates it.
	if created.Status != models.InstitutionStatusPending {
		t.Fatalf("expected status pending, got %q", created.Status)
	}
	if created.Plan != models.InstitutionPlanFree {
		t.Fatalf("expected plan free, got %q", created.Plan)
	}
	if created.Country != "Tanzania" {
		t.Fatalf("expected country Tanzania, got %q", created.Country)
	}
}

func TestCreateInstitution_RejectsDuplicateSlug(t *testing.T) {
	repo := newStubInstitutionRepo()
	repo.Create(&models.Institution{Name: "First", Slug: "first", Type: models.InstitutionTypeCollege})

	ctrl := NewInstitutionController(repo, nil)
	r := gin.New()
	r.POST("/institutions", ctrl.Create)

	body := `{"name":"Another","slug":"first","type":"college"}`
	req := httptest.NewRequest(http.MethodPost, "/institutions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a duplicate slug, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteInstitution_RefusesDefault(t *testing.T) {
	repo := newStubInstitutionRepo()
	repo.Create(&models.Institution{
		ID: models.DefaultInstitutionID, Name: models.DefaultInstitutionName,
		Slug: "default", Type: models.InstitutionTypeCollege,
	})

	ctrl := NewInstitutionController(repo, nil)
	r := gin.New()
	r.DELETE("/institutions/:id", ctrl.Delete)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/institutions/1", nil))

	// The default institution is the backfill target for existing data, so
	// deleting it would orphan unmoved records.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when deleting the default institution, got %d", w.Code)
	}
	if _, err := repo.GetByID(models.DefaultInstitutionID); err != nil {
		t.Fatal("default institution was deleted")
	}
}

func TestGetMe_ReturnsOwnInstitution(t *testing.T) {
	repo := newStubInstitutionRepo()
	repo.Create(&models.Institution{
		ID: tenantA, Name: "A University", Slug: "a-university",
		Type: models.InstitutionTypeUniversity, Status: models.InstitutionStatusActive,
	})
	repo.Create(&models.Institution{
		ID: tenantB, Name: "B College", Slug: "b-college",
		Type: models.InstitutionTypeCollege, Status: models.InstitutionStatusActive,
	})

	ctrl := NewInstitutionController(repo, nil)
	r := gin.New()
	r.GET("/institution/me", withTenant(ctrl.GetMe, tenantA))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/institution/me", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !contains(body, "A University") {
		t.Fatalf("expected own institution, got %s", body)
	}
	if contains(body, "B College") {
		t.Fatalf("response leaked another institution: %s", body)
	}
}

func TestGetMe_PlatformAdminHasNoSingleInstitution(t *testing.T) {
	repo := newStubInstitutionRepo()
	ctrl := NewInstitutionController(repo, nil)
	r := gin.New()
	r.GET("/institution/me", withTenant(ctrl.GetMe, 0))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/institution/me", nil))

	// A platform operator is not bound to one tenant; report that rather than
	// guessing or returning the first institution.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"scope":"platform"`) {
		t.Fatalf("expected platform scope, got %s", w.Body.String())
	}
}

func TestInstitution_TrialAndStatusHelpers(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name   string
		inst   models.Institution
		active bool
	}{
		{"active no trial", models.Institution{Status: models.InstitutionStatusActive}, true},
		{"active with future trial", models.Institution{Status: models.InstitutionStatusActive, TrialEndsAt: &future}, true},
		{"active with expired trial", models.Institution{Status: models.InstitutionStatusActive, TrialEndsAt: &past}, false},
		{"pending", models.Institution{Status: models.InstitutionStatusPending}, false},
		{"suspended", models.Institution{Status: models.InstitutionStatusSuspended}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.inst.IsActive(now); got != tc.active {
				t.Errorf("IsActive = %v, want %v", got, tc.active)
			}
		})
	}
}
