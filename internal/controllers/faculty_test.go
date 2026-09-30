package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/models"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// withTenant wraps a handler so the request context carries the tenant that
// TenantMiddleware would have set. Tests use this instead of repeating the
// context setup, and it makes the "tenant comes from the session, not the
// request" contract explicit.
func withTenant(handler gin.HandlerFunc, institutionID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		middlewares.SetTenant(c, institutionID, string(models.RoleAdmin))
		handler(c)
	}
}

func TestCreateFaculty_HappyPath(t *testing.T) {
	repo := newStubFacultyRepo()
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.POST("/faculties", withTenant(ctrl.CreateFaculty, tenantA))

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

func TestCreateFaculty_StampsSessionTenant(t *testing.T) {
	repo := newStubFacultyRepo()
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.POST("/faculties", withTenant(ctrl.CreateFaculty, tenantB))

	body := `{"name":"Business"}`
	req := httptest.NewRequest(http.MethodPost, "/faculties", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	// The faculty must land in the session's institution, not any value the
	// client could have supplied.
	for _, f := range repo.items {
		if f.Name != "Business" {
			continue
		}
		if f.InstitutionID != tenantB {
			t.Fatalf("expected faculty in institution %d, got %d", tenantB, f.InstitutionID)
		}
		return
	}
	t.Fatal("created faculty not found in repo")
}

func TestCreateFaculty_ValidationError(t *testing.T) {
	repo := newStubFacultyRepo()
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.POST("/faculties", withTenant(ctrl.CreateFaculty, tenantA))

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
	r.GET("/faculties/:id", withTenant(ctrl.GetFaculty, tenantA))

	req := httptest.NewRequest(http.MethodGet, "/faculties/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateFaculty_HappyPath(t *testing.T) {
	repo := newStubFacultyRepo()
	_ = repo.Create(tenantA, &models.Faculty{Name: "Old"})
	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.PUT("/faculties/:id", withTenant(ctrl.UpdateFaculty, tenantA))

	body := `{"name":"New Name","hod_email":"hod@example.com"}`
	req := httptest.NewRequest(http.MethodPut, "/faculties/1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestFaculty_CrossTenantIsolation is the isolation test for this controller:
// institution B's faculty must be invisible and immutable to institution A.
func TestFaculty_CrossTenantIsolation(t *testing.T) {
	repo := newStubFacultyRepo()
	// Faculty 1 belongs to A, faculty 2 belongs to B.
	repo.seed(&models.Faculty{Name: "A-Faculty", InstitutionID: tenantA})
	repo.seed(&models.Faculty{Name: "B-Faculty", InstitutionID: tenantB})

	ctrl := NewFacultyController(repo)
	r := gin.New()
	r.GET("/faculties", withTenant(ctrl.GetAllFaculties, tenantA))
	r.GET("/faculties/:id", withTenant(ctrl.GetFaculty, tenantA))
	r.PUT("/faculties/:id", withTenant(ctrl.UpdateFaculty, tenantA))
	r.DELETE("/faculties/:id", withTenant(ctrl.DeleteFaculty, tenantA))

	t.Run("list only returns own tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/faculties?limit=50", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := w.Body.String()
		if contains(body, "B-Faculty") {
			t.Fatalf("tenant A list leaked tenant B faculty: %s", body)
		}
		if !contains(body, "A-Faculty") {
			t.Fatalf("tenant A list missing its own faculty: %s", body)
		}
	})

	t.Run("get other tenant is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/faculties/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-tenant get, got %d", w.Code)
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/faculties/2", bytes.NewBufferString(`{"name":"Hijacked"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-tenant update, got %d", w.Code)
		}
		if repo.items[2].Name != "B-Faculty" {
			t.Fatalf("cross-tenant update mutated the record: name=%q", repo.items[2].Name)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/faculties/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-tenant delete, got %d", w.Code)
		}
		if _, ok := repo.items[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
