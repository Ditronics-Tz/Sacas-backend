package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
)

// newMyTimetableRouter emulates the protected group: the tenant middleware has
// already resolved user_id and institution_id onto the context. The session
// identity is what the handler must use, never client input.
func newMyTimetableRouter(
	staffRepo *stubStaffRepo,
	ttRepo *stubTimetableRepo,
	sessionUserID uint,
	sessionInstitution uint,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ctrl := NewTimetableController(ttRepo, staffRepo, newStubClassRepo(), newStubRoomRepo(), nil)
	r := gin.New()
	r.GET("/api/protected/timetable/my", func(c *gin.Context) {
		c.Set("user_id", sessionUserID)
		c.Set("institution_id", sessionInstitution)
		c.Set("role", string(models.RoleUser))
		ctrl.GetMyTimetable(c)
	})
	return r
}

// TestGetMyTimetable_UsesSessionStaffLinkOnly verifies:
//   - the staff record is resolved from the session user_id, not any client input
//   - the lookup is scoped to the session's institution
//   - response shape is the frontend-compatible {"timetables": [...]}
func TestGetMyTimetable_UsesSessionStaffLinkOnly(t *testing.T) {
	sessionUserID := uint(7)
	staffID := uint(42)

	staffRepo := newStubStaffRepo()
	staffRepo.seedStaff(&models.Staff{
		ID: staffID, Name: "Dr A", UserID: &sessionUserID, InstitutionID: tenantA,
	})

	ttRepo := newStubTimetableRepo()
	ttRepo.seed(&models.Timetable{
		StaffID: staffID, InstitutionID: tenantA,
		Day: models.Monday, StartTime: "08:00", EndTime: "09:00",
	})
	// Another staff member's timetable exists in the same institution — must
	// never be returned.
	ttRepo.seed(&models.Timetable{
		StaffID: 999, InstitutionID: tenantA,
		Day: models.Tuesday, StartTime: "10:00", EndTime: "11:00",
	})

	r := newMyTimetableRouter(staffRepo, ttRepo, sessionUserID, tenantA)

	// Attempt to manipulate via query param must be ignored.
	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/my?staff_id=999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Timetables []models.Timetable `json:"timetables"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if len(body.Timetables) != 1 || body.Timetables[0].StaffID != staffID {
		t.Fatalf("expected only own timetable (staff %d), got %+v", staffID, body.Timetables)
	}
}

func TestGetMyTimetable_NoStaffLinked(t *testing.T) {
	staffRepo := newStubStaffRepo()
	ttRepo := newStubTimetableRepo()
	r := newMyTimetableRouter(staffRepo, ttRepo, 7, tenantA)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/my", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for user without linked staff, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestGetMyTimetable_CrossTenantStaffLink is the isolation test for this
// endpoint: a session pinned to institution A must not resolve a staff record
// that belongs to institution B, even though the user_id genuinely matches.
// This is the case that plain user_id resolution would get wrong.
func TestGetMyTimetable_CrossTenantStaffLink(t *testing.T) {
	sessionUserID := uint(7)

	staffRepo := newStubStaffRepo()
	// The user's staff record lives at institution B.
	staffRepo.seedStaff(&models.Staff{
		ID: 42, Name: "Dr B-Staff", UserID: &sessionUserID, InstitutionID: tenantB,
	})

	ttRepo := newStubTimetableRepo()
	ttRepo.seed(&models.Timetable{
		StaffID: 42, InstitutionID: tenantB,
		Day: models.Monday, StartTime: "08:00", EndTime: "09:00",
	})

	r := newMyTimetableRouter(staffRepo, ttRepo, sessionUserID, tenantA)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/protected/timetable/my", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 resolving a cross-tenant staff link, got %d body=%s", w.Code, w.Body.String())
	}
	if contains(w.Body.String(), "Dr B-Staff") {
		t.Fatalf("response leaked another tenant's staff: %s", w.Body.String())
	}
}
