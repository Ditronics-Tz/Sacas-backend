package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/models"
)

// nilSliceTimetableRepo returns a nil slice from GetByCourse so the
// controller's nil guard is exercised. It embeds the shared tenant-aware stub
// and only overrides the one method under test.
type nilSliceTimetableRepo struct {
	*stubTimetableRepo
}

func (r *nilSliceTimetableRepo) GetByCourse(institutionID, courseID uint) ([]models.Timetable, error) {
	return nil, nil
}

// byCourseRouter wires GetTimetableByCourse the way the protected timetable
// group does: an institution admin scoped to tenantA.
func byCourseRouter(repo *stubTimetableRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ctrl := NewTimetableController(repo, newStubStaffRepo(), newStubClassRepo(), newStubRoomRepo(), nil)
	r := gin.New()
	r.GET("/api/protected/timetable/by-course/:course_id", withTenant(ctrl.GetTimetableByCourse, tenantA))
	return r
}

// byCourseRepo builds a repo with two entries for course 5, all in tenantA.
func byCourseRepo() *stubTimetableRepo {
	repo := newStubTimetableRepo()
	repo.classCourse[10] = 5
	repo.classCourse[11] = 5
	repo.seed(&models.Timetable{
		ClassID: 10, StaffID: 1, RoomID: 1, InstitutionID: tenantA,
		Day: models.Monday, StartTime: "08:00", EndTime: "09:00",
	})
	repo.seed(&models.Timetable{
		ClassID: 11, StaffID: 2, RoomID: 2, InstitutionID: tenantA,
		Day: models.Tuesday, StartTime: "09:00", EndTime: "10:00",
	})
	return repo
}

// 1. Success path — course with classes that have timetable entries.
func TestGetTimetableByCourse_Success(t *testing.T) {
	r := byCourseRouter(byCourseRepo())

	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/5", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Timetables []models.Timetable `json:"timetables"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, w.Body.String())
	}
	if len(body.Timetables) != 2 {
		t.Fatalf("expected 2 timetables, got %d body=%s", len(body.Timetables), w.Body.String())
	}
}

// 2. Course with no classes — empty array, not null.
func TestGetTimetableByCourse_Empty(t *testing.T) {
	r := byCourseRouter(byCourseRepo())

	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	// Must be [] not null — raw string check required by ticket.
	if strings.Contains(w.Body.String(), `"timetables":null`) {
		t.Fatalf("expected empty array, got null: %s", w.Body.String())
	}
	var body struct {
		Timetables []models.Timetable `json:"timetables"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Timetables == nil {
		t.Fatalf("timetables is nil, want empty slice")
	}
	if len(body.Timetables) != 0 {
		t.Fatalf("expected 0 timetables, got %d", len(body.Timetables))
	}
}

// Also verify the nil-slice guard: repo returns nil (simulating uninitialized map miss).
func TestGetTimetableByCourse_NilSliceGuard(t *testing.T) {
	ctrl := NewTimetableController(
		&nilSliceTimetableRepo{stubTimetableRepo: newStubTimetableRepo()},
		newStubStaffRepo(), newStubClassRepo(), newStubRoomRepo(), nil,
	)
	r := gin.New()
	r.GET("/api/protected/timetable/by-course/:course_id", withTenant(ctrl.GetTimetableByCourse, tenantA))

	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/7", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), `"timetables":null`) {
		t.Fatalf("controller must not return null for nil slice, got %s", w.Body.String())
	}
}

// 3a. Invalid: non-numeric :course_id -> 400
func TestGetTimetableByCourse_InvalidID(t *testing.T) {
	r := byCourseRouter(byCourseRepo())

	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric course_id, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestGetTimetableByCourse_CrossTenantIsolation is the isolation test for this
// controller: a session scoped to institution A must not see the entries that
// belong to institution B, even for the same numeric course ID.
func TestGetTimetableByCourse_CrossTenantIsolation(t *testing.T) {
	repo := byCourseRepo()
	// Institution B has its own class 20 on the same course ID 5.
	repo.classCourse[20] = 5
	repo.seed(&models.Timetable{
		ClassID: 20, StaffID: 9, RoomID: 9, InstitutionID: tenantB,
		Day: models.Wednesday, StartTime: "11:00", EndTime: "12:00",
	})

	r := byCourseRouter(repo)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/5", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"class_id":20`) {
		t.Fatalf("response leaked another tenant's timetable entries: %s", w.Body.String())
	}

	var body struct {
		Timetables []models.Timetable `json:"timetables"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Timetables) != 2 {
		t.Fatalf("expected only tenant A's 2 entries, got %d", len(body.Timetables))
	}
}

// 3b. Unauthorized — no token -> 401, role=user -> 403 (via AdminMiddleware)
func TestGetTimetableByCourse_Unauthorized(t *testing.T) {
	secret := "test-jwt-secret-for-by-course-rbac-32"
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("ENV", "development")

	ctrl := NewTimetableController(byCourseRepo(), newStubStaffRepo(), newStubClassRepo(), newStubRoomRepo(), nil)

	// Helper to build a router that mirrors the real protected+timetable grouping
	buildRouter := func() *gin.Engine {
		r := gin.New()
		r.GET("/api/protected/timetable/by-course/:course_id",
			middlewares.JWTAuthMiddleware(),
			middlewares.AdminMiddleware(),
			ctrl.GetTimetableByCourse,
		)
		return r
	}

	t.Run("no token -> 401", func(t *testing.T) {
		r := buildRouter()
		req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/5", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 without token, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("role=user -> 403", func(t *testing.T) {
		r := buildRouter()
		token := signTokenForTest(t, string(models.RoleUser), secret)
		req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/5", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for role=user, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("role=administrator -> 200", func(t *testing.T) {
		r := buildRouter()
		token := signTokenForTest(t, string(models.RoleAdmin), secret)
		req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/5", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for administrator, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func signTokenForTest(t *testing.T, role, secret string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(1),
		"email":   "test@example.com",
		"role":    role,
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	})
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}
