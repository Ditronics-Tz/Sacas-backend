package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
)

// TestGetMyStaff_* covers GET /api/protected/me/staff
func TestGetMyStaff_WithLinkedStaff(t *testing.T) {
	userID := uint(7)
	staff := &models.Staff{ID: 42, Name: "Dr A", Email: "a@example.com", FacultyID: 1, UserID: &userID}
	repo := &staffRepoWithUser{staff: staff}
	ctrl := NewStaffController(repo, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/protected/me/staff", func(c *gin.Context) {
		c.Set("user_id", float64(userID))
		ctrl.GetMyStaff(c)
	})

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
	repo := &staffRepoWithUser{staff: nil}
	ctrl := NewStaffController(repo, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/protected/me/staff", func(c *gin.Context) {
		c.Set("user_id", float64(7))
		ctrl.GetMyStaff(c)
	})

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
	ctrl := NewStaffController(&staffRepoWithUser{}, nil)
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
