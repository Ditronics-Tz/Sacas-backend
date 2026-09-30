package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
)

// stubExamRepo is an in-memory exam repository that enforces tenant scoping and
// the lifecycle transition rules, so the tests exercise the real constraints.
type stubExamRepo struct {
	items  map[uint]*models.Exam
	nextID uint
}

func newStubExamRepo() *stubExamRepo {
	return &stubExamRepo{items: map[uint]*models.Exam{}, nextID: 1}
}

func (r *stubExamRepo) Create(institutionID uint, exam *models.Exam) error {
	if !repositories.IsPlatformScope(institutionID) {
		exam.InstitutionID = institutionID
	}
	exam.ID = r.nextID
	r.nextID++
	cp := *exam
	r.items[exam.ID] = &cp
	return nil
}

func (r *stubExamRepo) GetByID(institutionID, id uint) (*models.Exam, error) {
	exam, ok := r.items[id]
	if !ok || !repositories.InTenant(exam.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *exam
	return &cp, nil
}

func (r *stubExamRepo) Update(institutionID uint, exam *models.Exam) error {
	existing, ok := r.items[exam.ID]
	if !ok || !repositories.InTenant(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *exam
	cp.InstitutionID = existing.InstitutionID
	r.items[exam.ID] = &cp
	return nil
}

func (r *stubExamRepo) Delete(institutionID, id uint) error {
	exam, ok := r.items[id]
	if !ok || !repositories.InTenant(exam.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubExamRepo) GetAll(institutionID uint, limit, offset int) ([]models.Exam, error) {
	var out []models.Exam
	for _, e := range r.items {
		if repositories.InTenant(e.InstitutionID, institutionID) {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (r *stubExamRepo) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Exam, error) {
	var out []models.Exam
	for _, e := range r.items {
		if repositories.InTenant(e.InstitutionID, institutionID) && e.CourseID == courseID {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (r *stubExamRepo) GetByStatus(institutionID uint, status models.ExamStatus, limit, offset int) ([]models.Exam, error) {
	var out []models.Exam
	for _, e := range r.items {
		if repositories.InTenant(e.InstitutionID, institutionID) && e.Status == status {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (r *stubExamRepo) GetByDateRange(institutionID uint, from, to string, limit, offset int) ([]models.Exam, error) {
	return r.GetAll(institutionID, limit, offset)
}

func (r *stubExamRepo) SetStatus(institutionID, id uint, status models.ExamStatus, userID uint, at time.Time) (bool, error) {
	exam, ok := r.items[id]
	if !ok || !repositories.InTenant(exam.InstitutionID, institutionID) {
		return false, gorm.ErrRecordNotFound
	}
	if !exam.Status.CanTransitionTo(status) {
		return false, repositories.ErrInvalidExamTransition
	}
	cp := *exam
	cp.Status = status
	switch status {
	case models.ExamStatusPublished:
		cp.PublishedAt = &at
		cp.PublishedByID = &userID
	case models.ExamStatusApproved:
		cp.ApprovedAt = &at
		cp.ApprovedByID = &userID
	}
	r.items[id] = &cp
	return true, nil
}

func overlaps(a *models.Exam, start, end string) bool {
	return a.StartTime < end && start < a.EndTime
}

func (r *stubExamRepo) CheckRoomConflicts(institutionID, roomID uint, date, start, end string, excludeID uint) ([]models.Exam, error) {
	var out []models.Exam
	for _, e := range r.items {
		if e.RoomID == nil || *e.RoomID != roomID || e.ID == excludeID {
			continue
		}
		if repositories.InTenant(e.InstitutionID, institutionID) && e.ExamDate == date && overlaps(e, start, end) {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (r *stubExamRepo) CheckInvigilatorConflicts(institutionID, invigilatorID uint, date, start, end string, excludeID uint) ([]models.Exam, error) {
	var out []models.Exam
	for _, e := range r.items {
		if e.InvigilatorID == nil || *e.InvigilatorID != invigilatorID || e.ID == excludeID {
			continue
		}
		if repositories.InTenant(e.InstitutionID, institutionID) && e.ExamDate == date && overlaps(e, start, end) {
			out = append(out, *e)
		}
	}
	return out, nil
}

// examFixture wires the exam controller with in-memory stubs.
type examFixture struct {
	ctrl    *ExamController
	exams   *stubExamRepo
	courses *stubCourseRepo
	rooms   *stubRoomRepo
	staff   *stubStaffRepo
	classes *stubClassRepo
	modules *stubModuleRepo
	audit   *recordingAuditRepo
	router  *gin.Engine
}

func newExamFixture(t *testing.T, instID uint, callerID uint) *examFixture {
	t.Helper()

	exams := newStubExamRepo()
	courses := newStubCourseRepo()
	rooms := newStubRoomRepo()
	staff := newStubStaffRepo()
	classes := newStubClassRepo()
	modules := newStubModuleRepo()
	audit := &recordingAuditRepo{}

	ctrl := NewExamController(exams, courses, modules, classes, rooms, staff,
		services.NewAuditRecorder(audit))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", float64(callerID))
		c.Set("role", string(models.RoleAdmin))
		c.Set("institution_id", instID)
		c.Next()
	})
	r.POST("/exams", ctrl.Create)
	r.GET("/exams", ctrl.GetAll)
	r.GET("/exams/:id", ctrl.Get)
	r.PUT("/exams/:id", ctrl.Update)
	r.DELETE("/exams/:id", ctrl.Delete)
	r.POST("/exams/:id/status/:status", ctrl.SetStatus)

	return &examFixture{
		ctrl: ctrl, exams: exams, courses: courses, rooms: rooms,
		staff: staff, classes: classes, modules: modules, audit: audit, router: r,
	}
}

func validExamBody() string {
	return `{"title":"Final Exam","type":"final","course_id":1,"exam_date":"2026-11-02",` +
		`"start_time":"09:00","end_time":"11:00","room_id":1,"invigilator_id":1,"max_marks":100}`
}

func TestExam_CreateHappyPath(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})
	f.rooms.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	f.staff.seedStaff(&models.Staff{ID: 1, Name: "Dr A", Email: "a@x.com", InstitutionID: tenantA})

	w := postJSON(f.router, "/exams", validExamBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if len(f.exams.items) != 1 {
		t.Fatalf("expected 1 exam, got %d", len(f.exams.items))
	}
	for _, e := range f.exams.items {
		if e.InstitutionID != tenantA {
			t.Errorf("the institution was not stamped from the session: %d", e.InstitutionID)
		}
		if e.Status != models.ExamStatusDraft {
			t.Errorf("expected a new exam to be a draft, got %s", e.Status)
		}
	}
}

func TestExam_RejectsCrossTenantReferences(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	// Every referenced record belongs to institution B.
	f.courses.seed(&models.Course{ID: 1, Name: "B-Course", FacultyID: 1, InstitutionID: tenantB})
	f.rooms.seed(&models.Room{ID: 1, Name: "B-Room", Capacity: 50, InstitutionID: tenantB})
	f.staff.seedStaff(&models.Staff{ID: 1, Name: "Dr B", Email: "b@x.com", InstitutionID: tenantB})

	w := postJSON(f.router, "/exams", validExamBody())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a cross-tenant course, got %d body=%s", w.Code, w.Body.String())
	}
	if len(f.exams.items) != 0 {
		t.Fatal("an exam was created against another institution's records")
	}
}

func TestExam_ValidatesPayload(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})

	cases := map[string]string{
		"bad type":         `{"title":"X","type":"nonsense","course_id":1,"exam_date":"2026-11-02","start_time":"09:00","end_time":"11:00"}`,
		"end before start": `{"title":"X","type":"final","course_id":1,"exam_date":"2026-11-02","start_time":"11:00","end_time":"09:00"}`,
		"bad date":         `{"title":"X","type":"final","course_id":1,"exam_date":"02-11-2026","start_time":"09:00","end_time":"11:00"}`,
		// 30 February: a date that parses structurally but does not exist. An
		// exam on a day that never happens is a real-world failure.
		"impossible date": `{"title":"X","type":"final","course_id":1,"exam_date":"2026-02-30","start_time":"09:00","end_time":"11:00"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w := postJSON(f.router, "/exams", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestExam_RoomConflictIsRefused(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})
	f.rooms.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	f.staff.seedStaff(&models.Staff{ID: 1, Name: "Dr A", Email: "a@x.com", InstitutionID: tenantA})

	if w := postJSON(f.router, "/exams", validExamBody()); w.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	// A second exam overlapping the same room.
	w := postJSON(f.router, "/exams", validExamBody())
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a room clash, got %d body=%s", w.Code, w.Body.String())
	}
	if len(f.exams.items) != 1 {
		t.Fatalf("the clashing exam was created: %d exams", len(f.exams.items))
	}
}

// TestExam_PublishRequiresScheduling: an exam with no room or invigilator is not
// a real sitting, and approving it would let results be entered against it.
func TestExam_PublishRequiresScheduling(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})
	roomID, invigID := uint(1), uint(1)

	// Create an unscheduled exam.
	w := postJSON(f.router, "/exams",
		`{"title":"Draft","type":"final","course_id":1,"exam_date":"2026-11-02","start_time":"09:00","end_time":"11:00"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	_ = roomID
	_ = invigID

	// Scheduling, publishing, and approving in sequence.
	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/exams/1/status/scheduled", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("schedule: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/exams/1/status/published", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 publishing an unscheduled exam, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExam_LifecycleAndAudit(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})
	f.rooms.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	f.staff.seedStaff(&models.Staff{ID: 1, Name: "Dr A", Email: "a@x.com", InstitutionID: tenantA})

	if w := postJSON(f.router, "/exams", validExamBody()); w.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	for _, status := range []string{"scheduled", "published", "approved"} {
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/exams/1/status/"+status, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d body=%s", status, w.Code, w.Body.String())
		}
	}

	// All three transitions are on the record, in order.
	wantOrder := []models.AuditAction{
		models.AuditExamSchedule, models.AuditExamPublish, models.AuditExamApprove,
	}
	seen := 0
	for _, e := range f.audit.entries {
		if seen < len(wantOrder) && e.Action == wantOrder[seen] {
			seen++
		}
	}
	if seen != len(wantOrder) {
		t.Errorf("expected the lifecycle actions in order, matched %d of %d: %+v",
			seen, len(wantOrder), f.audit.entries)
	}
}

// TestExam_ApprovedIsTerminal guards the sign-off.
func TestExam_ApprovedIsTerminal(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})
	f.rooms.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	f.staff.seedStaff(&models.Staff{ID: 1, Name: "Dr A", Email: "a@x.com", InstitutionID: tenantA})

	if w := postJSON(f.router, "/exams", validExamBody()); w.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d", w.Code)
	}
	for _, status := range []string{"scheduled", "published", "approved"} {
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/exams/1/status/"+status, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", status, w.Code)
		}
	}

	// Withdraw attempt from approved must be refused.
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/exams/1/status/scheduled", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 withdrawing an approved exam, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExam_ApprovedCannotBeEdited(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})
	f.rooms.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	f.staff.seedStaff(&models.Staff{ID: 1, Name: "Dr A", Email: "a@x.com", InstitutionID: tenantA})

	if w := postJSON(f.router, "/exams", validExamBody()); w.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d", w.Code)
	}
	for _, status := range []string{"scheduled", "published", "approved"} {
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/exams/1/status/"+status, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", status, w.Code)
		}
	}

	w := putJSON(f.router, "/exams/1", `{"title":"Changed"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 editing an approved exam, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExam_SetStatusRejectsDraftTarget(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.courses.seed(&models.Course{ID: 1, Name: "Eng", FacultyID: 1, InstitutionID: tenantA})
	f.rooms.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	f.staff.seedStaff(&models.Staff{ID: 1, Name: "Dr A", Email: "a@x.com", InstitutionID: tenantA})
	if w := postJSON(f.router, "/exams", validExamBody()); w.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	// A caller must not be able to reach the draft target through a route whose
	// permission is for publishing.
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/exams/1/status/draft", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a draft target, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExam_GetAllIsTenantScoped(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.exams.Create(tenantA, &models.Exam{ID: 1, Title: "A Exam", InstitutionID: tenantA, Status: models.ExamStatusDraft})
	f.exams.Create(tenantB, &models.Exam{ID: 2, Title: "B Exam", InstitutionID: tenantB, Status: models.ExamStatusDraft})

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/exams", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if contains(w.Body.String(), "B Exam") {
		t.Fatalf("another institution's exam leaked: %s", w.Body.String())
	}
	if !contains(w.Body.String(), "A Exam") {
		t.Fatalf("own exam missing: %s", w.Body.String())
	}
}

func TestExam_CrossTenantGetIsNotFound(t *testing.T) {
	f := newExamFixture(t, tenantA, 1)
	f.exams.Create(tenantB, &models.Exam{ID: 2, Title: "B Exam", InstitutionID: tenantB, Status: models.ExamStatusDraft})

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/exams/2", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}
