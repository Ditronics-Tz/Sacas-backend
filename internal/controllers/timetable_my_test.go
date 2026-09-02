package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"go_boilerplate/internal/models"
)

// stubTimetableRepo implements repositories.TimetableRepository for tests and
// records which staff_id was queried, so we can assert the handler only ever
// uses the JWT-derived staff mapping.
type stubTimetableRepo struct {
	byStaff map[uint][]models.Timetable
	lastStaffID uint
}

func (r *stubTimetableRepo) Create(t *models.Timetable) error      { return nil }
func (r *stubTimetableRepo) GetByID(id uint) (*models.Timetable, error) { return nil, errNotFound }
func (r *stubTimetableRepo) Update(t *models.Timetable) error      { return nil }
func (r *stubTimetableRepo) Delete(id uint) error                  { return nil }
func (r *stubTimetableRepo) DeleteByClass(classID uint) error      { return nil }
func (r *stubTimetableRepo) ReplaceClassTimetable(classID uint, entries []models.Timetable) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepo) GetAll(limit, offset int) ([]models.Timetable, error) { return nil, nil }
func (r *stubTimetableRepo) GetByClass(classID uint) ([]models.Timetable, error)  { return nil, nil }
func (r *stubTimetableRepo) GetByStaff(staffID uint) ([]models.Timetable, error) {
	r.lastStaffID = staffID
	return r.byStaff[staffID], nil
}
func (r *stubTimetableRepo) GetByRoom(roomID uint) ([]models.Timetable, error) { return nil, nil }
func (r *stubTimetableRepo) GetByDay(day models.Weekday) ([]models.Timetable, error) { return nil, nil }
func (r *stubTimetableRepo) CheckConflicts(classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepo) GetByDateRange(startDate, endDate string) ([]models.Timetable, error) {
	return nil, nil
}
func (r *stubTimetableRepo) DB() *gorm.DB { return nil }

// staffRepoWithUser is a minimal StaffRepository stub returning a linked staff.
type staffRepoWithUser struct {
	staff *models.Staff
}

func (r *staffRepoWithUser) Create(s *models.Staff) error                { return nil }
func (r *staffRepoWithUser) GetByID(id uint) (*models.Staff, error)      { return nil, errNotFound }
func (r *staffRepoWithUser) GetByEmail(email string) (*models.Staff, error) { return nil, errNotFound }
func (r *staffRepoWithUser) GetByUserID(userID uint) (*models.Staff, error) {
	if r.staff != nil && r.staff.UserID != nil && *r.staff.UserID == userID {
		cp := *r.staff
		return &cp, nil
	}
	return nil, errNotFound
}
func (r *staffRepoWithUser) Update(s *models.Staff) error                { return nil }
func (r *staffRepoWithUser) Delete(id uint) error                        { return nil }
func (r *staffRepoWithUser) GetAll(limit, offset int) ([]models.Staff, error) { return nil, nil }
func (r *staffRepoWithUser) GetByFaculty(facultyID uint, limit, offset int) ([]models.Staff, error) {
	return nil, nil
}
func (r *staffRepoWithUser) GetWithModules(id uint) (*models.Staff, error) { return nil, errNotFound }
func (r *staffRepoWithUser) UpdatePreferences(id uint, preferences string) error { return nil }
func (r *staffRepoWithUser) AssignModule(staffID, moduleID uint) error   { return nil }
func (r *staffRepoWithUser) UnassignModule(staffID, moduleID uint) error { return nil }
func (r *staffRepoWithUser) ListModules(staffID uint) ([]models.Module, error) { return nil, nil }
func (r *staffRepoWithUser) ListStaffForModule(moduleID uint) ([]models.Staff, error) { return nil, nil }

func newMyTimetableRouter(staffRepo *staffRepoWithUser, ttRepo *stubTimetableRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ctrl := NewTimetableController(ttRepo, staffRepo, nil)
	r := gin.New()
	// Emulates the protected group: JWT middleware has already set user_id.
	r.GET("/api/protected/timetable/my", func(c *gin.Context) {
		c.Set("user_id", float64(7))
		ctrl.GetMyTimetable(c)
	})
	return r
}

// TestGetMyTimetable_UsesJWTStaffLinkOnly verifies:
//   - the staff record is resolved from the JWT user_id, not any client input
//   - response shape is the frontend-compatible {"timetables": [...]}
func TestGetMyTimetable_UsesJWTStaffLinkOnly(t *testing.T) {
	userID := uint(7)
	staffID := uint(42)
	ttRepo := &stubTimetableRepo{byStaff: map[uint][]models.Timetable{
		staffID: {{ID: 1, StaffID: staffID, Day: models.Monday, StartTime: "08:00", EndTime: "09:00"}},
	}}
	// Another staff member's timetable exists — must never be returned.
	ttRepo.byStaff[999] = []models.Timetable{{ID: 2, StaffID: 999}}

	staffRepo := &staffRepoWithUser{staff: &models.Staff{ID: staffID, Name: "Dr A", UserID: &userID}}
	r := newMyTimetableRouter(staffRepo, ttRepo)

	// Attempt to manipulate via query param must be ignored.
	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/my?staff_id=999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if ttRepo.lastStaffID != staffID {
		t.Fatalf("expected query for staff %d, got %d (client input may have leaked)", staffID, ttRepo.lastStaffID)
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
	ttRepo := &stubTimetableRepo{byStaff: map[uint][]models.Timetable{}}
	staffRepo := &staffRepoWithUser{staff: nil}
	r := newMyTimetableRouter(staffRepo, ttRepo)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/timetable/my", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for user without linked staff, got %d body=%s", w.Code, w.Body.String())
	}
}
