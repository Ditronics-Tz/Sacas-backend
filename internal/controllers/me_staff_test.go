package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
)

// newMyStaffRouter emulates the protected group: the tenant middleware has
// already put user_id and institution_id on the context.
func newMyStaffRouter(repo *stubStaffRepo, sessionUserID, sessionInstitution uint) *gin.Engine {
	ctrl := NewStaffController(repo, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/protected/me/staff", func(c *gin.Context) {
		c.Set("user_id", sessionUserID)
		c.Set("institution_id", sessionInstitution)
		c.Set("role", string(models.RoleUser))
		ctrl.GetMyStaff(c)
	})
	return r
}

// TestGetMyStaff_* covers GET /api/protected/me/staff
func TestGetMyStaff_WithLinkedStaff(t *testing.T) {
	userID := uint(7)
	repo := newStubStaffRepo()
	repo.seedStaff(&models.Staff{
		ID: 42, Name: "Dr A", Email: "a@example.com", FacultyID: 1,
		UserID: &userID, InstitutionID: tenantA,
	})
	r := newMyStaffRouter(repo, userID, tenantA)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/me/staff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Staff *models.Staff `json:"staff"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, w.Body.String())
	}
	if body.Staff == nil || body.Staff.ID != 42 {
		t.Fatalf("expected staff 42, got %+v", body.Staff)
	}
	if body.Staff.UserID == nil || *body.Staff.UserID != userID {
		t.Fatalf("expected UserID %d, got %v", userID, body.Staff.UserID)
	}
}

func TestGetMyStaff_NoLinkedStaff(t *testing.T) {
	r := newMyStaffRouter(newStubStaffRepo(), 7, tenantA)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/me/staff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for user without linked staff, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// must contain a clear message, not a 500 error; staff should be null/missing
	if _, ok := body["error"]; !ok {
		t.Fatalf("expected error field in 404 body, got %v", body)
	}
	if body["staff"] != nil {
		t.Fatalf("expected staff null/missing for 404, got %v", body["staff"])
	}
}

func TestGetMyStaff_Unauthenticated(t *testing.T) {
	ctrl := NewStaffController(newStubStaffRepo(), nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/protected/me/staff", ctrl.GetMyStaff)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/me/staff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when no user_id in context, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestGetMyStaff_CrossTenantLink is the isolation test for this endpoint: a
// session scoped to institution A must not resolve a staff record that belongs
// to institution B, even though the user_id genuinely matches.
func TestGetMyStaff_CrossTenantLink(t *testing.T) {
	userID := uint(7)
	repo := newStubStaffRepo()
	repo.seedStaff(&models.Staff{
		ID: 42, Name: "Dr B-Staff", Email: "b@example.com", FacultyID: 1,
		UserID: &userID, InstitutionID: tenantB,
	})
	r := newMyStaffRouter(repo, userID, tenantA)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/protected/me/staff", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 resolving a cross-tenant staff link, got %d body=%s", w.Code, w.Body.String())
	}
	if contains(w.Body.String(), "Dr B-Staff") {
		t.Fatalf("response leaked another tenant's staff: %s", w.Body.String())
	}
}
