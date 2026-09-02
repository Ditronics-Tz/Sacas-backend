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
	"gorm.io/gorm"

	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/models"
)

// stubTimetableRepoByCourse is a focused stub for GetByCourse tests.
// It reuses the same interface surface as stubTimetableRepo but allows
// per-test control over GetByCourse behavior.
type stubTimetableRepoByCourse struct {
	byCourse map[uint][]models.Timetable
	err      error
}

func (r *stubTimetableRepoByCourse) Create(t *models.Timetable) error { return nil }
func (r *stubTimetableRepoByCourse) GetByID(id uint) (*models.Timetable, error) {
	return nil, errNotFound
}
func (r *stubTimetableRepoByCourse) Update(t *models.Timetable) error { return nil }
func (r *stubTimetableRepoByCourse) Delete(id uint) error             { return nil }
func (r *stubTimetableRepoByCourse) DeleteByClass(classID uint) error { return nil }
func (r *stubTimetableRepoByCourse) ReplaceClassTimetable(classID uint, entries []models.Timetable) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) GetAll(limit, offset int) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) GetByClass(classID uint) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) GetByStaff(staffID uint) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) GetByCourse(courseID uint) ([]models.Timetable, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.byCourse != nil {
		if v, ok := r.byCourse[courseID]; ok {
			return v, nil
		}
	}
	return []models.Timetable{}, nil
}
func (r *stubTimetableRepoByCourse) GetByRoom(roomID uint) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) GetByDay(day models.Weekday) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) CheckConflicts(classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) GetByDateRange(startDate, endDate string) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepoByCourse) DB() *gorm.DB { return nil }

func newByCourseRouter(repo *stubTimetableRepoByCourse) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ctrl := NewTimetableController(repo, nil, nil)
	r := gin.New()
	r.GET("/api/protected/timetable/by-course/:course_id", ctrl.GetTimetableByCourse)
	return r
}

// 1. Success path — course with classes that have timetable entries.
func TestGetTimetableByCourse_Success(t *testing.T) {
	repo := &stubTimetableRepoByCourse{
		byCourse: map[uint][]models.Timetable{
			5: {
				{ID: 1, ClassID: 10, StaffID: 1, RoomID: 1, Day: models.Monday, StartTime: "08:00", EndTime: "09:00"},
				{ID: 2, ClassID: 11, StaffID: 2, RoomID: 2, Day: models.Tuesday, StartTime: "09:00", EndTime: "10:00"},
			},
		},
	}
	r := newByCourseRouter(repo)

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
	repo := &stubTimetableRepoByCourse{
		byCourse: map[uint][]models.Timetable{
			99: {}, // explicit empty
		},
	}
	r := newByCourseRouter(repo)

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
	// Override to return nil explicitly
	nilRepo := &nilSliceRepo{}
	r := newByCourseRouterRaw(nilRepo)

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

// nilSliceRepo returns nil slice to exercise the controller's nil guard.
type nilSliceRepo struct{ stubTimetableRepoByCourse }

func (r *nilSliceRepo) GetByCourse(courseID uint) ([]models.Timetable, error) { return nil, nil }

func newByCourseRouterRaw(repo *nilSliceRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	// adapt nilSliceRepo to the interface by wrapping
	ctrl := NewTimetableController(&nilSliceAdapter{inner: repo}, nil, nil)
	r := gin.New()
	r.GET("/api/protected/timetable/by-course/:course_id", ctrl.GetTimetableByCourse)
	return r
}

type nilSliceAdapter struct{ inner *nilSliceRepo }

func (a *nilSliceAdapter) Create(t *models.Timetable) error { return nil }
func (a *nilSliceAdapter) GetByID(id uint) (*models.Timetable, error) {
	return nil, errNotFound
}
func (a *nilSliceAdapter) Update(t *models.Timetable) error { return nil }
func (a *nilSliceAdapter) Delete(id uint) error             { return nil }
func (a *nilSliceAdapter) DeleteByClass(classID uint) error { return nil }
func (a *nilSliceAdapter) ReplaceClassTimetable(classID uint, entries []models.Timetable) ([]models.Timetable, error) {
	return nil, nil
}
func (a *nilSliceAdapter) GetAll(limit, offset int) ([]models.Timetable, error) {
	return nil, nil
}
func (a *nilSliceAdapter) GetByClass(classID uint) ([]models.Timetable, error) { return nil, nil }
func (a *nilSliceAdapter) GetByStaff(staffID uint) ([]models.Timetable, error) { return nil, nil }
func (a *nilSliceAdapter) GetByCourse(courseID uint) ([]models.Timetable, error) {
	return a.inner.GetByCourse(courseID)
}
func (a *nilSliceAdapter) GetByRoom(roomID uint) ([]models.Timetable, error) { return nil, nil }
func (a *nilSliceAdapter) GetByDay(day models.Weekday) ([]models.Timetable, error) {
	return nil, nil
}
func (a *nilSliceAdapter) CheckConflicts(classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error) {
	return nil, nil
}
func (a *nilSliceAdapter) GetByDateRange(startDate, endDate string) ([]models.Timetable, error) {
	return nil, nil
}
func (a *nilSliceAdapter) DB() *gorm.DB { return nil }

// 3a. Invalid: non-numeric :course_id -> 400
func TestGetTimetableByCourse_InvalidID(t *testing.T) {
	repo := &stubTimetableRepoByCourse{}
	r := newByCourseRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/by-course/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric course_id, got %d body=%s", w.Code, w.Body.String())
	}
}

// 3b. Unauthorized — no token -> 401, role=user -> 403 (via AdminMiddleware)
func TestGetTimetableByCourse_Unauthorized(t *testing.T) {
	secret := "test-jwt-secret-for-by-course-rbac-32"
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("ENV", "development")

	repo := &stubTimetableRepoByCourse{
		byCourse: map[uint][]models.Timetable{
			5: {{ID: 1, ClassID: 10, StaffID: 1, RoomID: 1, Day: models.Monday, StartTime: "08:00", EndTime: "09:00"}},
		},
	}
	ctrl := NewTimetableController(repo, nil, nil)

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
